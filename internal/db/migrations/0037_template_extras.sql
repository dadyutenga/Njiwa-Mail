-- 0037: template extras. Additive except that the two locale CHECKs (en, sw only) become a
-- pattern check, so a template can be written in any language.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT c.conrelid::regclass AS tbl, c.conname
           FROM pg_constraint c
           WHERE c.contype = 'c' AND c.conrelid IN ('template_contents'::regclass, 'messages'::regclass)
             AND pg_get_constraintdef(c.oid) LIKE '%locale%' LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', r.tbl, r.conname);
  END LOOP;
END $$;
ALTER TABLE template_contents ADD CONSTRAINT template_contents_locale_check CHECK (locale ~ '^[a-z]{2,3}(-[a-z0-9]{2,8})?$');
ALTER TABLE messages ADD CONSTRAINT messages_locale_check CHECK (locale ~ '^[a-z]{2,3}(-[a-z0-9]{2,8})?$');

-- Shared stylesheets of a project. A template version keeps a snapshot of the CSS it was saved
-- with, so changing or deleting a stylesheet never alters a published version.
CREATE TABLE IF NOT EXISTS template_styles (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
  css        text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);
ALTER TABLE template_versions
  ADD COLUMN IF NOT EXISTS style_name text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS style_css  text NOT NULL DEFAULT '';
