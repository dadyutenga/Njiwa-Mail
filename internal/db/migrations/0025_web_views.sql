-- 0025: "view in browser" copies. Additive. The HTML is encrypted (AAD is the
-- message id); a row is deleted with its message and after expires_at.
CREATE TABLE IF NOT EXISTS web_views (
  message_id  uuid PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  html_enc    bytea NOT NULL,
  subject     text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS web_views_expires ON web_views(expires_at);
