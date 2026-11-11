-- 0012: controlled transport tests (backlog C04).
-- A transport test is an ordinary Live message sent through the real queue
-- and worker. The flag is set only by the server (never by API input); when
-- the worker finishes such a message, the outcome is recorded as the
-- test_send check of the profile revision that actually sent it.
ALTER TABLE messages ADD COLUMN transport_test boolean NOT NULL DEFAULT false;
CREATE INDEX messages_transport_test ON messages (created_at DESC) WHERE transport_test;
