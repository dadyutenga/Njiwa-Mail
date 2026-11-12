-- F10: encryption at rest for inbound attachments. New files are sealed with
-- ENCRYPTION_KEY; rows written before this migration stay plaintext
-- (encrypted=false) until `njiwa-mail attachments encrypt` or the retention
-- job converts them.
ALTER TABLE inbound_attachments ADD COLUMN IF NOT EXISTS encrypted boolean NOT NULL DEFAULT false;
