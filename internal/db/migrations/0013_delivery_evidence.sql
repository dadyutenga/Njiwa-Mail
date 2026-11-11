-- 0013: delivery evidence (backlog F01, E08). Additive.
-- A message's state says what Njiwa did (submitted, failed, unknown). Delivery
-- evidence records what was observed afterwards, with its source, without
-- ever turning "submitted" into "delivered": a remote server accepting a
-- message is not inbox placement.
ALTER TABLE messages ADD COLUMN mta_queue_id text;
CREATE INDEX messages_mta_queue ON messages (mta_queue_id, submitted_at DESC) WHERE mta_queue_id IS NOT NULL;

CREATE TABLE delivery_events (
  id bigserial PRIMARY KEY,
  message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  source text NOT NULL CHECK (source IN ('smtp','mta_log','dsn','operator')),
  event text NOT NULL CHECK (event IN ('submitted','deferred','remote_accepted','bounced','complaint','expired_in_queue','operator_resolved')),
  smtp_status text NOT NULL DEFAULT '',
  remote_host text NOT NULL DEFAULT '',
  safe_reason text NOT NULL DEFAULT '',   -- never contains addresses or message content
  mta_queue_id text NOT NULL DEFAULT '',
  dedupe_key text NOT NULL UNIQUE,
  observed_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX delivery_events_message ON delivery_events (message_id, id);
