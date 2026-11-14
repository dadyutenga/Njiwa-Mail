-- 0028: a notification inbox per user. Additive.
-- Announcements are written by an operator for every user and can be
-- retracted; daily reports are made by the scheduler once per day; open
-- alerts are read from alerts. Each user's read and dismissed state is kept
-- per item, so dismissing something never hides it from anyone else.
CREATE TABLE IF NOT EXISTS announcements (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  title         text NOT NULL,
  body          text NOT NULL DEFAULT '',
  link          text NOT NULL DEFAULT '',
  created_by    text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now(),
  retracted_at  timestamptz
);

CREATE TABLE IF NOT EXISTS daily_reports (
  day         date PRIMARY KEY,
  data        jsonb NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS inbox_state (
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind          text NOT NULL CHECK (kind IN ('announcement','daily_report','alert')),
  item_id       text NOT NULL,
  read_at       timestamptz,
  dismissed_at  timestamptz,
  PRIMARY KEY (user_id, kind, item_id)
);
