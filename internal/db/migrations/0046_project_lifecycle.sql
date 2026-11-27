-- 0046: archive and delete projects. Additive columns. The two guards that keep
-- published template versions immutable now allow one thing: removing them
-- inside the transaction that deletes their project.
ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS archived_at timestamptz,
  ADD COLUMN IF NOT EXISTS purge_after timestamptz,
  ADD COLUMN IF NOT EXISTS enabled_before_archive boolean;

CREATE OR REPLACE FUNCTION dm_template_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('njiwa.deleting_project', true) = 'on' THEN RETURN OLD; END IF;
  IF OLD.published_at IS NOT NULL THEN
    RAISE EXCEPTION 'published template version is immutable' USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION dm_template_content_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE vid uuid;
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('njiwa.deleting_project', true) = 'on' THEN RETURN OLD; END IF;
  IF TG_OP = 'INSERT' THEN vid := NEW.version_id; ELSE vid := OLD.version_id; END IF;
  IF EXISTS (SELECT 1 FROM template_versions WHERE id = vid AND published_at IS NOT NULL) THEN
    RAISE EXCEPTION 'published template version is immutable' USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
