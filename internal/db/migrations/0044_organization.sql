-- 0044: first-run setup. The installation's organization, and a person's name.
-- Additive. An existing installation has users and no organization; setup stays
-- closed for it.
CREATE TABLE IF NOT EXISTS organization (
  id         smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  name       text NOT NULL,
  slug       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name text NOT NULL DEFAULT '';
