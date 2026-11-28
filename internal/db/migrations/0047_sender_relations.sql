-- 0047: senders get a purpose, a reply-to, can be disabled, and are linked to
-- templates and API keys. Additive, except that "one default per environment"
-- becomes "one default per environment and purpose". Existing defaults become
-- the default transactional senders.
ALTER TABLE sender_identities
  ADD COLUMN IF NOT EXISTS purpose text NOT NULL DEFAULT 'transactional'
    CHECK (purpose IN ('transactional','marketing','support','system')),
  ADD COLUMN IF NOT EXISTS reply_to text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disabled_at timestamptz;
DROP INDEX IF EXISTS sender_identities_one_default;
CREATE UNIQUE INDEX IF NOT EXISTS sender_identities_default_per_purpose
  ON sender_identities (environment_id, purpose) WHERE is_default;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS default_sender text NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_senders text[] NOT NULL DEFAULT '{}';
