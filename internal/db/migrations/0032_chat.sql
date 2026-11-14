-- 0032: live chat. Additive. Off unless CHAT_ENABLED is set.
-- Visitor details (name, email) are encrypted (AAD "chat-visitor:<id>") and so is
-- every message body (AAD "chat-msg:<id>"); the visitor's IP is kept only as a
-- keyed hash, and the visitor's session token only as a SHA-256 hash.
CREATE TABLE IF NOT EXISTS chat_widgets (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  environment_id  uuid NOT NULL,
  name            text NOT NULL,
  allowed_origins text[] NOT NULL DEFAULT '{}',
  title           text NOT NULL DEFAULT '',
  welcome         text NOT NULL DEFAULT '',
  offline_message text NOT NULL DEFAULT '',
  announcement    text NOT NULL DEFAULT '',
  links           jsonb NOT NULL DEFAULT '[]',
  reply_from      text NOT NULL DEFAULT '',
  enabled         boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, name)
);

CREATE TABLE IF NOT EXISTS chat_conversations (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  widget_id       uuid NOT NULL REFERENCES chat_widgets(id) ON DELETE CASCADE,
  environment_id  uuid NOT NULL,
  token_hash      bytea NOT NULL UNIQUE,
  visitor_enc     bytea NOT NULL,
  email_hash      bytea,
  email_masked    text NOT NULL DEFAULT '',
  ip_hash         bytea NOT NULL,
  state           text NOT NULL DEFAULT 'open' CHECK (state IN ('open','closed')),
  assigned_to     uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  last_message_at timestamptz NOT NULL DEFAULT now(),
  visitor_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS chat_conversations_env ON chat_conversations (environment_id, last_message_at DESC);

CREATE TABLE IF NOT EXISTS chat_messages (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id uuid NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
  environment_id  uuid NOT NULL,
  sender          text NOT NULL CHECK (sender IN ('visitor','agent')),
  agent_email     text NOT NULL DEFAULT '',
  body_enc        bytea NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  -- agent messages: when the visitor's open page received it, and when it was emailed instead
  delivered_at    timestamptz,
  emailed_at      timestamptz,
  -- visitor messages: when an agent opened the conversation after it
  seen_by_agent   boolean NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS chat_messages_conv ON chat_messages (conversation_id, created_at, id);
CREATE INDEX IF NOT EXISTS chat_messages_env ON chat_messages (environment_id, created_at, id);
CREATE INDEX IF NOT EXISTS chat_messages_undelivered ON chat_messages (conversation_id) WHERE sender='agent' AND delivered_at IS NULL AND emailed_at IS NULL;
