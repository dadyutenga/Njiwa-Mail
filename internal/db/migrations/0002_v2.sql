-- Njiwa Mail v2: admin users, DB templates, inbound, bounces, webhooks, DNS checks.

-- ---------------------------------------------------------------- admin
CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL UNIQUE CHECK (email = lower(email)),
  password_hash text NOT NULL,
  role text NOT NULL CHECK (role IN ('owner','admin','viewer')),
  disabled boolean NOT NULL DEFAULT false,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  token_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  ip text NOT NULL DEFAULT '',
  user_agent text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE audit_log (
  id bigserial PRIMARY KEY,
  at timestamptz NOT NULL DEFAULT now(),
  user_id uuid,
  api_key_id uuid,
  actor text NOT NULL,
  action text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id text NOT NULL DEFAULT '',
  details jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at ON audit_log (at DESC);

CREATE TABLE settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alerts (
  id bigserial PRIMARY KEY,
  kind text NOT NULL,
  summary text NOT NULL,
  details jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz,
  acknowledged_by text
);
CREATE INDEX alerts_open ON alerts (created_at DESC) WHERE acknowledged_at IS NULL;

-- ---------------------------------------------------------------- templates
-- project_id NULL = shared template available to every project. A project's
-- own template with the same slug takes precedence.
CREATE TABLE templates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid REFERENCES projects(id),
  slug text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
  description text NOT NULL DEFAULT '',
  default_ttl_seconds int NOT NULL DEFAULT 3600,
  max_ttl_seconds int NOT NULL DEFAULT 86400,
  archived boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX templates_slug ON templates (COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), slug);

CREATE TABLE template_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id uuid NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
  version int NOT NULL CHECK (version > 0),
  variable_schema jsonb NOT NULL DEFAULT '[]',
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (template_id, version)
);

-- One row per locale of a version.
CREATE TABLE template_contents (
  version_id uuid NOT NULL REFERENCES template_versions(id) ON DELETE CASCADE,
  locale text NOT NULL CHECK (locale IN ('en','sw')),
  subject text NOT NULL,
  html_body text NOT NULL,
  text_body text NOT NULL,
  PRIMARY KEY (version_id, locale)
);

-- Published versions are immutable, enforced in the database as well as the app.
CREATE FUNCTION dm_template_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.published_at IS NOT NULL THEN
    RAISE EXCEPTION 'published template version is immutable' USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER template_versions_immutable BEFORE UPDATE OR DELETE ON template_versions
  FOR EACH ROW EXECUTE FUNCTION dm_template_version_guard();

CREATE FUNCTION dm_template_content_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE vid uuid;
BEGIN
  IF TG_OP = 'INSERT' THEN vid := NEW.version_id; ELSE vid := OLD.version_id; END IF;
  IF EXISTS (SELECT 1 FROM template_versions WHERE id = vid AND published_at IS NOT NULL) THEN
    RAISE EXCEPTION 'published template version is immutable' USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER template_contents_immutable BEFORE INSERT OR UPDATE OR DELETE ON template_contents
  FOR EACH ROW EXECUTE FUNCTION dm_template_content_guard();

-- ---------------------------------------------------------------- messages
ALTER TABLE messages RENAME COLUMN message_id_header TO smtp_message_id;
ALTER TABLE messages
  ADD COLUMN locale text NOT NULL DEFAULT 'en' CHECK (locale IN ('en','sw')),
  ADD COLUMN template_version_id uuid REFERENCES template_versions(id),
  ADD COLUMN bounce_state text NOT NULL DEFAULT 'none' CHECK (bounce_state IN ('none','soft','hard')),
  ALTER COLUMN api_key_id DROP NOT NULL;  -- NULL = operator test-send from the admin UI
CREATE INDEX messages_recipient ON messages (environment_id, recipient_hash, created_at DESC);
CREATE INDEX messages_env_created ON messages (environment_id, created_at DESC);

ALTER TABLE api_keys ADD COLUMN last_used_at timestamptz;

ALTER TABLE worker_heartbeats ADD COLUMN role text NOT NULL DEFAULT 'worker';

-- ---------------------------------------------------------------- webhooks
CREATE TABLE webhook_endpoints (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  url text NOT NULL,
  secret_enc bytea NOT NULL,
  events text[] NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  consecutive_failures int NOT NULL DEFAULT 0,
  disabled_reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);
CREATE INDEX webhook_endpoints_env ON webhook_endpoints (environment_id);

-- The outbox. A row's id is the event id receivers deduplicate on; `payload`
-- holds only the safe `data` object (ids, states, reason codes).
CREATE TABLE webhook_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  endpoint_id uuid NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
  event_type text NOT NULL,
  payload jsonb NOT NULL,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','succeeded','dead')),
  attempts int NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_token uuid,
  lease_expires_at timestamptz,
  last_response_code int,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries (next_attempt_at) WHERE state IN ('pending','sending');
CREATE INDEX webhook_deliveries_endpoint ON webhook_deliveries (endpoint_id, created_at DESC);

-- dm_emit writes one outbox row per enabled endpoint of the environment that
-- subscribes to the event. It is called inside the transaction that makes the
-- state change, so an event exists if and only if the change committed.
CREATE FUNCTION dm_emit(p_env uuid, p_type text, p_data jsonb, p_endpoint uuid DEFAULT NULL) RETURNS int
LANGUAGE sql AS $$
  WITH ins AS (
    INSERT INTO webhook_deliveries (endpoint_id, event_type, payload)
    SELECT w.id, p_type, p_data FROM webhook_endpoints w
    WHERE w.environment_id = p_env AND w.enabled AND p_type = ANY (w.events)
      AND (p_endpoint IS NULL OR w.id = p_endpoint)
    RETURNING 1)
  SELECT count(*)::int FROM ins
$$;

-- ---------------------------------------------------------------- inbound
CREATE TABLE inbound_addresses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  domain text NOT NULL CHECK (domain = lower(domain)),
  local_part text NOT NULL CHECK (local_part = lower(local_part)),  -- '*' = every address under the domain
  enabled boolean NOT NULL DEFAULT true,
  webhook_endpoint_id uuid REFERENCES webhook_endpoints(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (domain, local_part),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);

-- Raw mail accepted by the SMTP listener. The row and the file on disk are
-- both durable before the listener answers 250; parsing happens afterwards.
CREATE TABLE inbound_spool (
  id uuid PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('inbound','bounce')),
  envelope_from text NOT NULL,
  envelope_to text[] NOT NULL,
  path text NOT NULL,
  size int NOT NULL,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','processing','done','failed')),
  attempts int NOT NULL DEFAULT 0,
  lease_expires_at timestamptz,
  error_code text NOT NULL DEFAULT '',
  received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX inbound_spool_due ON inbound_spool (received_at) WHERE state IN ('pending','processing');

CREATE TABLE inbound_messages (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  address_id uuid REFERENCES inbound_addresses(id) ON DELETE SET NULL,
  spool_id uuid NOT NULL,
  envelope_from text NOT NULL,
  envelope_to text NOT NULL,
  header_from text NOT NULL DEFAULT '',
  subject text NOT NULL DEFAULT '',
  text_enc bytea,           -- encrypted at rest; NULL after retention
  html_enc bytea,
  headers jsonb NOT NULL DEFAULT '{}',
  spam_score real,
  raw_size int NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  retention_expires_at timestamptz NOT NULL,
  bodies_purged_at timestamptz,
  UNIQUE (spool_id, envelope_to),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id)
);
CREATE INDEX inbound_messages_env ON inbound_messages (environment_id, received_at DESC);
CREATE INDEX inbound_messages_retention ON inbound_messages (retention_expires_at) WHERE bodies_purged_at IS NULL;

CREATE TABLE inbound_attachments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  inbound_message_id uuid NOT NULL REFERENCES inbound_messages(id) ON DELETE CASCADE,
  filename text NOT NULL,
  content_type text NOT NULL,
  size int NOT NULL,
  storage_path text NOT NULL,
  sha256 text NOT NULL
);
CREATE INDEX inbound_attachments_message ON inbound_attachments (inbound_message_id);

-- ---------------------------------------------------------------- bounces
CREATE TABLE bounces (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id uuid REFERENCES messages(id) ON DELETE SET NULL,  -- NULL when uncorrelated
  project_id uuid,
  environment_id uuid,
  recipient_hash bytea,
  recipient_masked text NOT NULL DEFAULT '',
  type text NOT NULL CHECK (type IN ('hard','soft','policy','unknown')),
  status_code text NOT NULL DEFAULT '',
  diagnostic text NOT NULL DEFAULT '',
  dedupe_hash bytea NOT NULL UNIQUE,
  received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bounces_received ON bounces (received_at DESC);
CREATE INDEX bounces_message ON bounces (message_id);

ALTER TABLE suppressions
  ADD COLUMN source text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','hard_bounce')),
  ADD COLUMN bounce_id uuid REFERENCES bounces(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------- dns
CREATE TABLE dns_checks (
  id bigserial PRIMARY KEY,
  domain text NOT NULL,
  record_type text NOT NULL,
  expected text NOT NULL,
  observed text NOT NULL,
  pass boolean NOT NULL,
  checked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dns_checks_latest ON dns_checks (domain, record_type, checked_at DESC);
