-- 0034: who may open a project, and as what. Additive.
-- Installation operators (users.installation_operator) may open every project and are
-- treated as owners everywhere, so they need no rows. Everyone else needs a row.
-- Roles, weakest first: viewer (read), developer (send and edit content), admin (keys,
-- domains, senders, webhooks, settings, personal-data tools), owner (also members).
-- Before this migration every signed-in user could open every project, so existing
-- non-operator users are made owners of the existing projects: nobody loses access.
CREATE TABLE IF NOT EXISTS project_members (
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       text NOT NULL CHECK (role IN ('viewer','developer','admin','owner')),
  added_by   text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, user_id)
);
CREATE INDEX IF NOT EXISTS project_members_user ON project_members (user_id);

INSERT INTO project_members (project_id, user_id, role, added_by)
SELECT p.id, u.id, 'owner', 'migration 0034'
FROM projects p CROSS JOIN users u
WHERE NOT u.installation_operator
ON CONFLICT DO NOTHING;
