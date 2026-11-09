-- Overview and bounce-rate queries filter submitted_at every few seconds.
-- Without this index both are sequential scans of messages (measured 59 ms
-- and 15 ms at 80k rows).
CREATE INDEX messages_submitted_at ON messages (submitted_at) WHERE submitted_at IS NOT NULL;

-- The dispatcher orders due mail by high priority, then next_attempt_at, and
-- only needs a few rows. An index in that order lets it stop after the limit
-- instead of sorting every queued row (measured 21 ms / 4977 buffers → 0.1 ms / 16).
CREATE INDEX messages_claim_order ON messages ((priority = 'high') DESC, next_attempt_at)
  WHERE state IN ('queued', 'retry_wait');
