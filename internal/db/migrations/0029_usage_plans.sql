-- 0029: usage plans and monthly quotas. Additive.
-- A plan is a named set of limits. Assigning it copies the limits onto the
-- project (and editing the plan updates its projects), so sending keeps
-- reading the project's own columns. 0 means unlimited.
CREATE TABLE IF NOT EXISTS usage_plans (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name                  text NOT NULL UNIQUE,
  description           text NOT NULL DEFAULT '',
  send_per_minute       int NOT NULL CHECK (send_per_minute > 0),
  send_per_day          int NOT NULL CHECK (send_per_day > 0),
  recipient_per_minute  int NOT NULL CHECK (recipient_per_minute > 0),
  recipient_per_hour    int NOT NULL CHECK (recipient_per_hour > 0),
  bulk_max_per_campaign int NOT NULL DEFAULT 5000 CHECK (bulk_max_per_campaign >= 0),
  bulk_max_per_day      int NOT NULL DEFAULT 10000 CHECK (bulk_max_per_day >= 0),
  monthly_messages      int NOT NULL DEFAULT 0 CHECK (monthly_messages >= 0),
  max_contacts          int NOT NULL DEFAULT 0 CHECK (max_contacts >= 0),
  created_at            timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS plan_id uuid REFERENCES usage_plans(id) ON DELETE SET NULL;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS monthly_messages int NOT NULL DEFAULT 0 CHECK (monthly_messages >= 0);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS max_contacts int NOT NULL DEFAULT 0 CHECK (max_contacts >= 0);

-- Live messages accepted per project and calendar month (UTC), counted in
-- the accept transaction, bulk included. Sandbox mail is never counted.
CREATE TABLE IF NOT EXISTS monthly_usage (
  project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  month       date NOT NULL,
  messages    int NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, month)
);
