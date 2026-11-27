-- 0043: a role can say how it is doing, not only that it is alive. Used by the
-- SMTP relay (starting, running, error, and the settings it really serves).
-- Additive; an empty state means a plain heartbeat, as before.
ALTER TABLE worker_heartbeats
  ADD COLUMN IF NOT EXISTS state text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS detail text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS info jsonb NOT NULL DEFAULT '{}';
