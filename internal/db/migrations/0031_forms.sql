-- 0031: contact forms. Additive. Off unless FORMS_ENABLED is set.
-- A submission's fields are encrypted (AAD "form-sub:<id>") and so are
-- uploaded files (AAD "form-file:<id>"); the sender address is kept as a
-- keyed hash and a masked form, the visitor's IP only as a keyed hash.
CREATE TABLE IF NOT EXISTS forms (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  environment_id  uuid NOT NULL,
  name            text NOT NULL,
  allowed_origins text[] NOT NULL DEFAULT '{}',
  notify_emails   text[] NOT NULL DEFAULT '{}',
  digest          text NOT NULL DEFAULT 'none' CHECK (digest IN ('none','immediate','hourly','daily')),
  redirect_url    text NOT NULL DEFAULT '',
  reply_from      text NOT NULL DEFAULT '',
  require_nonce   boolean NOT NULL DEFAULT false,
  enabled         boolean NOT NULL DEFAULT true,
  last_digest_at  timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, name)
);

CREATE TABLE IF NOT EXISTS form_submissions (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  form_id       uuid NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  environment_id uuid NOT NULL,
  received_at   timestamptz NOT NULL DEFAULT now(),
  data_enc      bytea NOT NULL,
  sender_hash   bytea NOT NULL,
  sender_masked text NOT NULL,
  ip_hash       bytea NOT NULL,
  spam_score    int NOT NULL DEFAULT 0,
  spam_reasons  jsonb NOT NULL DEFAULT '[]',
  action        text NOT NULL CHECK (action IN ('allow','flag','quarantine')),
  state         text NOT NULL DEFAULT 'new' CHECK (state IN ('new','open','replied','closed','spam')),
  assigned_to   uuid REFERENCES users(id) ON DELETE SET NULL,
  notified_at   timestamptz,
  replied_at    timestamptz
);
CREATE INDEX IF NOT EXISTS form_submissions_env ON form_submissions (environment_id, received_at DESC);
CREATE INDEX IF NOT EXISTS form_submissions_pending ON form_submissions (form_id) WHERE notified_at IS NULL AND action IN ('allow','flag');

CREATE TABLE IF NOT EXISTS form_files (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  submission_id uuid NOT NULL REFERENCES form_submissions(id) ON DELETE CASCADE,
  filename      text NOT NULL,
  content_type  text NOT NULL,
  size          int NOT NULL,
  data_enc      bytea NOT NULL
);

-- Single-use nonces for pages that fetch one before posting.
CREATE TABLE IF NOT EXISTS form_nonces (
  nonce_hash  bytea PRIMARY KEY,
  form_id     uuid NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  issued_at   timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz
);

-- Workspace spam filters. Every submission is checked against them.
CREATE TABLE IF NOT EXISTS form_filters (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL,
  environment_id uuid NOT NULL,
  kind           text NOT NULL CHECK (kind IN ('keyword','phrase','regex','email','domain','ip')),
  pattern        text NOT NULL,
  action         text NOT NULL CHECK (action IN ('flag','quarantine','reject')),
  enabled        boolean NOT NULL DEFAULT true,
  created_at     timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE
);
