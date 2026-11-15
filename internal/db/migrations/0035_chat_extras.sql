-- 0035: live chat extras. Additive.
-- identity_secret_enc: a per-chat secret (sealed, AAD "chat-identity:<id>") with which the
-- site's own server signs who a visitor is, so the email can be trusted; require_identity
-- refuses visitors who cannot show a valid signature.
ALTER TABLE chat_widgets
  ADD COLUMN IF NOT EXISTS identity_secret_enc bytea,
  ADD COLUMN IF NOT EXISTS require_identity boolean NOT NULL DEFAULT false;
ALTER TABLE chat_conversations
  ADD COLUMN IF NOT EXISTS identity_verified boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS banned_at timestamptz;

-- A banned visitor: the address a conversation came from (keyed hash) and the email they gave
-- (keyed hash). Starting a chat with either is refused in that environment.
CREATE TABLE IF NOT EXISTS chat_bans (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  environment_id uuid NOT NULL,
  ip_hash        bytea,
  email_hash     bytea,
  reason         text NOT NULL DEFAULT '',
  created_by     text NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK (ip_hash IS NOT NULL OR email_hash IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS chat_bans_ip ON chat_bans (environment_id, ip_hash);
CREATE INDEX IF NOT EXISTS chat_bans_email ON chat_bans (environment_id, email_hash);

-- Canned replies an agent can insert into an answer.
CREATE TABLE IF NOT EXISTS chat_snippets (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL,
  environment_id uuid NOT NULL,
  title          text NOT NULL,
  body           text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, title)
);
