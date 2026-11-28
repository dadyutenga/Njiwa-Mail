-- 0048: newsletter issues. An issue is written as blocks; sending or
-- scheduling it creates a campaign, which carries audience, delivery,
-- unsubscribe and results. Additive.
CREATE TABLE IF NOT EXISTS newsletter_issues (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL,
  environment_id uuid NOT NULL,
  name           text NOT NULL,
  subject        text NOT NULL DEFAULT '',
  preview_text   text NOT NULL DEFAULT '',
  sender_email   text NOT NULL DEFAULT '',
  list_id        uuid REFERENCES contact_lists(id) ON DELETE SET NULL,
  segment_id     uuid REFERENCES contact_segments(id) ON DELETE SET NULL,
  blocks         jsonb NOT NULL DEFAULT '[]',
  track          boolean NOT NULL DEFAULT false,
  campaign_id    uuid REFERENCES campaigns(id) ON DELETE SET NULL,
  created_by     text NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS newsletter_issues_env ON newsletter_issues (environment_id, created_at DESC);
