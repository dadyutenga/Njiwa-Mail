-- 0041: report schedules. Additive.
-- Replaces the single daily-report email setting (report_email_settings, kept but no
-- longer read) with any number of schedules. Each mails one report from the
-- catalogue (daily or weekly summary) to its own recipients, from its own sender,
-- for all projects or one, at a chosen hour (UTC). last_sent_period is the day
-- (daily) or the Monday of the week (weekly) last mailed, so a period goes out once.
CREATE TABLE IF NOT EXISTS report_schedules (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  report             text NOT NULL CHECK (report IN ('daily_summary', 'weekly_summary')),
  enabled            boolean NOT NULL DEFAULT true,
  recipients         text[] NOT NULL CHECK (cardinality(recipients) BETWEEN 1 AND 20),
  sender_identity_id uuid REFERENCES sender_identities(id) ON DELETE SET NULL,
  project_id         uuid REFERENCES projects(id) ON DELETE CASCADE,
  send_hour          smallint NOT NULL DEFAULT 6 CHECK (send_hour BETWEEN 0 AND 23),
  last_sent_period   date,
  last_sent_at       timestamptz,
  last_error         text NOT NULL DEFAULT '',
  -- A period is mailed only if its send time is at or after this moment, so a new
  -- or changed schedule never mails an old report straight away.
  active_since       timestamptz NOT NULL DEFAULT now(),
  created_by         text NOT NULL DEFAULT '',
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         text NOT NULL DEFAULT '',
  updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Carry the existing setting over as the first schedule, sending as soon as the
-- report is made (hour 0), as before.
INSERT INTO report_schedules (name, report, enabled, recipients, sender_identity_id, send_hour,
  last_sent_period, active_since, created_by, updated_by, updated_at)
SELECT 'Daily report', 'daily_summary', enabled, recipients, sender_identity_id, 0,
  last_emailed_day, 'epoch', updated_by, updated_by, updated_at
FROM report_email_settings
WHERE id = 1 AND cardinality(recipients) BETWEEN 1 AND 20
  AND NOT EXISTS (SELECT 1 FROM report_schedules);
