-- 0010: installation-level transport profiles (backlog A05/A06/A07).
-- Additive only. The singleton mail_server row is left untouched so an older
-- binary keeps working against this schema (rollback boundary: the profile
-- tables are ignored by the old code, and SaveMailServer keeps writing the row).

-- ---------------------------------------------------------------- installation
CREATE TABLE installation_settings (
  id int PRIMARY KEY CHECK (id = 1),
  schema_version int NOT NULL DEFAULT 1,
  wizard jsonb NOT NULL DEFAULT '{}',
  previous_profile_id uuid,           -- kept for rollback after a mode switch
  lock_version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO installation_settings(id) VALUES (1);

-- ---------------------------------------------------------------- profiles
-- One row per way of getting mail out. Exactly one may be active. Secrets are
-- sealed with the application key and never returned by any API.
CREATE TABLE transport_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  mode text NOT NULL CHECK (mode IN ('managed_mta','existing_smtp')),
  provider_type text NOT NULL DEFAULT 'generic' CHECK (provider_type IN ('generic','mailcow')),
  label text NOT NULL DEFAULT '',
  config_revision bigint NOT NULL DEFAULT 1,
  config_json jsonb NOT NULL DEFAULT '{}',
  encrypted_secret bytea,             -- SMTP password
  encrypted_provider_secret bytea,    -- optional provider (Mailcow) API key
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','needs_action','ready','degraded','retired')),
  is_active boolean NOT NULL DEFAULT false,
  source text NOT NULL DEFAULT 'operator' CHECK (source IN ('operator','legacy_mail_server','environment')),
  last_tested_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_transport ON transport_profiles ((true)) WHERE is_active;

ALTER TABLE installation_settings
  ADD CONSTRAINT installation_previous_profile FOREIGN KEY (previous_profile_id) REFERENCES transport_profiles(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------- checks
-- A check belongs to one profile at one config revision. A pass for another
-- profile, or for an older revision, never makes the current one ready.
CREATE TABLE installation_checks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  profile_id uuid NOT NULL REFERENCES transport_profiles(id) ON DELETE CASCADE,
  config_revision bigint NOT NULL,
  check_key text NOT NULL,
  result text NOT NULL CHECK (result IN ('pass','fail','skipped')),
  problem_code text NOT NULL DEFAULT '',
  safe_details jsonb NOT NULL DEFAULT '{}',
  source text NOT NULL DEFAULT 'operator',
  checked_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz
);
CREATE INDEX installation_checks_profile ON installation_checks (profile_id, check_key, checked_at DESC);

-- ---------------------------------------------------------------- installer receipts
-- Evidence that an approved installer plan was applied. Holds a plan digest
-- and result codes only: never script bodies or secrets.
CREATE TABLE installer_receipts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  profile_id uuid REFERENCES transport_profiles(id) ON DELETE SET NULL,
  plan_digest text NOT NULL,
  approved_by text NOT NULL DEFAULT '',
  action text NOT NULL,
  result_code text NOT NULL,
  config_backup_ref text NOT NULL DEFAULT '',
  applied_version text NOT NULL DEFAULT '',
  verified_at timestamptz,
  rollback_state text NOT NULL DEFAULT 'none' CHECK (rollback_state IN ('none','available','rolled_back')),
  created_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- message pinning
-- Which profile revision accepted the message. Evidence for reconciliation and
-- for refusing to re-route jobs silently after a mode switch.
ALTER TABLE messages
  ADD COLUMN transport_profile_id uuid,
  ADD COLUMN transport_revision bigint;

-- ---------------------------------------------------------------- legacy migration (A06)
-- A working singleton becomes the active existing_smtp profile with its sealed
-- secrets copied byte for byte (same AAD, so they open unchanged). Its old
-- last_test is NOT carried over: readiness must be re-proven for this revision.
INSERT INTO transport_profiles(mode, provider_type, label, config_revision, config_json, encrypted_secret, encrypted_provider_secret, status, is_active, source)
SELECT 'existing_smtp',
       CASE WHEN mailcow_api_url <> '' THEN 'mailcow' ELSE 'generic' END,
       'Existing SMTP (migrated)', 1,
       jsonb_build_object('host', smtp_host, 'port', smtp_port, 'tls_mode', smtp_tls_mode, 'username', smtp_username,
                          'test_sender', test_sender, 'inbound_listener', inbound_listener, 'mailcow_api_url', mailcow_api_url),
       smtp_password_enc, mailcow_api_key_enc, 'needs_action', true, 'legacy_mail_server'
FROM mail_server WHERE id = 1 AND smtp_host <> '';
