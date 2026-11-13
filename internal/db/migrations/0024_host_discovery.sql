-- 0024: the latest read-only host discovery (njiwa-mail discover -record).
-- Additive. The report holds no secrets: ports, program names, service
-- evidence, certificate names and expiry, and the recommendation.
ALTER TABLE installation_settings ADD COLUMN IF NOT EXISTS discovery jsonb NOT NULL DEFAULT '{}';
ALTER TABLE installation_settings ADD COLUMN IF NOT EXISTS discovered_at timestamptz;
