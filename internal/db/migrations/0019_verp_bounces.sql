-- F06: how a bounce was tied to our message: 'verp' (signed return path),
-- 'header' (our message id in the returned headers, the older method), 'verp_invalid'
-- (a signed return path whose signature did not verify: never correlated) or 'none'.
ALTER TABLE bounces ADD COLUMN IF NOT EXISTS correlated_by text NOT NULL DEFAULT '';
