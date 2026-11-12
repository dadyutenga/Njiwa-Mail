-- 0014: signed-mail evidence (backlog D06). A published DKIM key only proves
-- that a key is published. These columns record that a message from the
-- domain was received with a valid, aligned DKIM signature, and when.
ALTER TABLE domains
  ADD COLUMN dkim_verified_at timestamptz,
  ADD COLUMN dkim_verified_selector text NOT NULL DEFAULT '',
  ADD COLUMN dkim_verified_source text NOT NULL DEFAULT '' CHECK (dkim_verified_source IN ('','upload','inbound'));
