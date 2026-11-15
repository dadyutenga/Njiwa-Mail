-- 0033: the daily report by email. Additive.
-- One row of operator settings: who gets the report, and which sender identity
-- it is sent from. With no sender chosen, the default sender of a Live
-- environment is used. last_emailed_day makes sure a day's report is mailed once.
CREATE TABLE IF NOT EXISTS report_email_settings (
  id                int PRIMARY KEY CHECK (id = 1),
  enabled           boolean NOT NULL DEFAULT false,
  recipients        text[] NOT NULL DEFAULT '{}',
  sender_identity_id uuid REFERENCES sender_identities(id) ON DELETE SET NULL,
  last_emailed_day  date,
  updated_by        text NOT NULL DEFAULT '',
  updated_at        timestamptz NOT NULL DEFAULT now()
);
INSERT INTO report_email_settings(id) VALUES (1) ON CONFLICT DO NOTHING;
