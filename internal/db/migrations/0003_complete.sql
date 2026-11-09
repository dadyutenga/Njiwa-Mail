-- Features from the product specification, still served by the /v1 API.
-- scheduled and cancelled are honest queue states. delivered is not a state.

ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_state_check;
ALTER TABLE messages ADD CONSTRAINT messages_state_check
  CHECK (state IN ('queued','submitting','submitted','retry_wait','failed','unknown','expired','captured','scheduled','cancelled'));

ALTER TABLE messages
  ADD COLUMN priority text NOT NULL DEFAULT 'normal' CHECK (priority IN ('high','normal')),
  ADD COLUMN send_at timestamptz,
  ADD COLUMN reply_to text NOT NULL DEFAULT '',
  ADD COLUMN cc text[] NOT NULL DEFAULT '{}',
  ADD COLUMN bcc text[] NOT NULL DEFAULT '{}';

CREATE INDEX messages_scheduled ON messages (send_at) WHERE state = 'scheduled';

CREATE TABLE message_attachments (
  id uuid PRIMARY KEY,
  message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  filename text NOT NULL,
  content_type text NOT NULL,
  size int NOT NULL CHECK (size >= 0),
  data_enc bytea NOT NULL
);

CREATE TABLE domains (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  name text NOT NULL CHECK (name = lower(name)),
  purpose text NOT NULL CHECK (purpose IN ('sending','inbound')),
  verify_token text NOT NULL,
  verified_at timestamptz,
  spf_ok boolean NOT NULL DEFAULT false,
  dkim_ok boolean NOT NULL DEFAULT false,
  dmarc_ok boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name, purpose)
);

ALTER TABLE api_keys
  ADD COLUMN ip_allowlist text[] NOT NULL DEFAULT '{}',
  ADD COLUMN rate_per_minute int NOT NULL DEFAULT 0 CHECK (rate_per_minute >= 0);

ALTER TABLE inbound_addresses
  ADD COLUMN route text NOT NULL DEFAULT 'store' CHECK (route IN ('store','webhook','forward')),
  ADD COLUMN forward_to text NOT NULL DEFAULT '';

ALTER TABLE inbound_messages
  ADD COLUMN in_reply_to_message_id uuid REFERENCES messages(id),
  ADD COLUMN search_tsv tsvector;
CREATE INDEX inbound_messages_search ON inbound_messages USING gin (search_tsv);

ALTER TABLE users
  ADD COLUMN totp_secret_enc bytea,
  ADD COLUMN totp_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN recovery_hashes text[] NOT NULL DEFAULT '{}';

CREATE TABLE password_resets (
  token_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);

CREATE TABLE alert_channels (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind text NOT NULL CHECK (kind IN ('email','webhook')),
  target text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
