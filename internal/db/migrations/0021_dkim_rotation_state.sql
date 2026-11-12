-- Mail host screens: the state of a staged DKIM rotation as last recorded by
-- the installer (`installer dkim-rotate ... -record`), so the dashboard can show it.
ALTER TABLE installation_settings ADD COLUMN IF NOT EXISTS dkim_rotation jsonb NOT NULL DEFAULT '{}';
