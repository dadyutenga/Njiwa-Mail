-- 0045: temporary inboxes are removed (owner's decision, 2026-11-28).
-- DESTRUCTIVE. Deleted: every temporary inbox address and the mail it received.
-- Dropped: inbound_addresses.temporary, inbound_addresses.expires_at (and its
-- index) and projects.temp_inbox_limit. Permanent mailboxes and their mail are
-- untouched. A migration cannot delete files, so the attachment files of the
-- deleted mail are listed in removed_inbound_files for the scheduler.
CREATE TABLE IF NOT EXISTS removed_inbound_files (message_id uuid PRIMARY KEY);
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = current_schema() AND table_name = 'inbound_addresses' AND column_name = 'temporary') THEN
    INSERT INTO removed_inbound_files
      SELECT m.id FROM inbound_messages m JOIN inbound_addresses a ON a.id = m.address_id WHERE a.temporary
      ON CONFLICT DO NOTHING;
    DELETE FROM inbound_messages WHERE address_id IN (SELECT id FROM inbound_addresses WHERE temporary);
    DELETE FROM inbound_addresses WHERE temporary;
  END IF;
END $$;
ALTER TABLE inbound_addresses DROP COLUMN IF EXISTS temporary, DROP COLUMN IF EXISTS expires_at;
ALTER TABLE projects DROP COLUMN IF EXISTS temp_inbox_limit;
DELETE FROM job_runs WHERE name = 'temporary_inboxes';
