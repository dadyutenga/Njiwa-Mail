-- 0036: form extras. Additive.
-- Tags and a priority on each submission, and per-form routing rules that set them (and the
-- assignee) when a submission arrives.
ALTER TABLE form_submissions
  ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS priority text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high'));
CREATE INDEX IF NOT EXISTS form_submissions_tags ON form_submissions USING gin (tags);

CREATE TABLE IF NOT EXISTS form_rules (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  form_id      uuid NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  position     int NOT NULL DEFAULT 0,
  name         text NOT NULL,
  field        text NOT NULL DEFAULT '',          -- '' = any field
  op           text NOT NULL CHECK (op IN ('contains','equals','regex','exists','domain')),
  value        text NOT NULL DEFAULT '',
  set_priority text NOT NULL DEFAULT '' CHECK (set_priority IN ('','low','normal','high')),
  add_tags     text[] NOT NULL DEFAULT '{}',
  assign_to    uuid REFERENCES users(id) ON DELETE SET NULL,
  stop         boolean NOT NULL DEFAULT false,    -- later rules are skipped after this one matches
  enabled      boolean NOT NULL DEFAULT true,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS form_rules_form ON form_rules (form_id, position);
