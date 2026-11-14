-- 0026: HTTP ingest of inbound mail, signed with a per-environment secret.
-- Additive. The secret is encrypted (AAD "ingest:<environment id>").
CREATE TABLE IF NOT EXISTS inbound_ingest (
  environment_id uuid PRIMARY KEY REFERENCES environments(id) ON DELETE CASCADE,
  secret_enc     bytea NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_used_at   timestamptz
);
