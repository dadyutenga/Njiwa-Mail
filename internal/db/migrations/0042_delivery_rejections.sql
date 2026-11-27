-- 0042: honest delivery readiness. Keeps what the mail server answered when it
-- refused a submission, and when a profile's settings last changed. Additive:
-- new columns, indexes and a trigger; the delivery_events event list only
-- gains 'rejected'. An older binary keeps working.
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS smtp_reject_code int,
  ADD COLUMN IF NOT EXISTS smtp_reject_problem text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS smtp_reject_reason text NOT NULL DEFAULT ''; -- sanitized: no addresses or tokens

ALTER TABLE delivery_events ADD COLUMN IF NOT EXISTS problem_code text NOT NULL DEFAULT '';
ALTER TABLE delivery_events DROP CONSTRAINT IF EXISTS delivery_events_event_check;
ALTER TABLE delivery_events ADD CONSTRAINT delivery_events_event_check
  CHECK (event IN ('submitted','deferred','remote_accepted','bounced','complaint','expired_in_queue','operator_resolved','rejected'));
CREATE INDEX IF NOT EXISTS delivery_events_recent ON delivery_events (event, observed_at DESC);

ALTER TABLE transport_profiles ADD COLUMN IF NOT EXISTS config_changed_at timestamptz;
CREATE OR REPLACE FUNCTION dm_profile_config_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.config_changed_at := now();
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS transport_profiles_config_changed ON transport_profiles;
CREATE TRIGGER transport_profiles_config_changed BEFORE UPDATE ON transport_profiles
  FOR EACH ROW WHEN (NEW.config_revision IS DISTINCT FROM OLD.config_revision)
  EXECUTE FUNCTION dm_profile_config_changed();
