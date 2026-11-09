CREATE TABLE projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  send_per_minute int NOT NULL DEFAULT 30,
  send_per_day int NOT NULL DEFAULT 500,
  recipient_per_minute int NOT NULL DEFAULT 3,
  recipient_per_hour int NOT NULL DEFAULT 10,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE environments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  mode text NOT NULL CHECK (mode IN ('sandbox','live')),
  approved_origins text[] NOT NULL DEFAULT '{}',
  restrict_recipients boolean NOT NULL DEFAULT true,
  recipient_allowlist text[] NOT NULL DEFAULT '{}',
  enabled boolean NOT NULL DEFAULT true,
  UNIQUE (project_id, mode),
  UNIQUE (id, project_id)
);

CREATE TABLE api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  prefix text NOT NULL UNIQUE,
  secret_hash bytea NOT NULL,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  scopes text[] NOT NULL,
  label text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  revoked_at timestamptz,
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);

CREATE TABLE sender_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  email text NOT NULL CHECK (email = lower(email)),
  display_name text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (environment_id, email),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);

CREATE TABLE messages (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  api_key_id uuid NOT NULL REFERENCES api_keys(id),
  template text NOT NULL,
  template_version int NOT NULL,
  state text NOT NULL CHECK (state IN ('queued','submitting','submitted','retry_wait','failed','unknown','expired','captured')),
  payload_enc bytea,
  recipient_hash bytea NOT NULL,
  request_hmac bytea NOT NULL,
  idempotency_key text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}',
  expires_at timestamptz NOT NULL,
  attempts int NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_token uuid,
  lease_expires_at timestamptz,
  smtp_reply text,
  message_id_header text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  submitted_at timestamptz,
  completed_at timestamptz,
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id),
  UNIQUE (project_id, environment_id, idempotency_key)
);
CREATE INDEX messages_dispatch ON messages (next_attempt_at) WHERE state IN ('queued','retry_wait');
CREATE INDEX messages_leases ON messages (lease_expires_at) WHERE state = 'submitting';
CREATE INDEX messages_created ON messages (created_at);
CREATE INDEX messages_state ON messages (state);

CREATE TABLE message_events (
  id bigserial PRIMARY KEY,
  message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  event_type text NOT NULL,
  reason_code text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX message_events_message ON message_events (message_id, id);

CREATE TABLE suppressions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  recipient_hash bytea NOT NULL,
  recipient_masked text NOT NULL,
  reason text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (environment_id, recipient_hash),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);

CREATE TABLE rate_limit_buckets (
  scope_key text NOT NULL,
  window_start timestamptz NOT NULL,
  count int NOT NULL,
  PRIMARY KEY (scope_key, window_start)
);

CREATE TABLE sandbox_captures (
  message_id uuid PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  raw_enc bytea NOT NULL,
  expires_at timestamptz NOT NULL
);

CREATE TABLE worker_heartbeats (
  worker_id text PRIMARY KEY,
  last_seen timestamptz NOT NULL
);
