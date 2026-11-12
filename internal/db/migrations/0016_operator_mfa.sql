-- F09: installation-operator permission and optional TOTP multi-factor sign-in.
-- Every existing user keeps today's access: they are all installation operators.
-- New users are operators by default too; a restricted admin is created explicitly.
ALTER TABLE users ADD COLUMN IF NOT EXISTS installation_operator boolean NOT NULL DEFAULT true;

-- TOTP (RFC 6238). The secret is sealed with ENCRYPTION_KEY; it is pending until totp_enabled.
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret_enc bytea;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled boolean NOT NULL DEFAULT false;
-- Highest accepted time step: a code can be used once (replay protection).
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_last_step bigint NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_failed integer NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_locked_until timestamptz;
-- Keyed hashes of single-use recovery codes.
ALTER TABLE users ADD COLUMN IF NOT EXISTS recovery_hashes text[] NOT NULL DEFAULT '{}';
