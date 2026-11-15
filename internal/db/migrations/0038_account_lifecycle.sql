-- 0038: self-service account lifecycle. Additive.
-- A user can ask for their account to be deleted; it is removed after a grace period unless
-- they cancel. The row stays usable during the grace period.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS deletion_requested_at timestamptz,
  ADD COLUMN IF NOT EXISTS deletion_due_at timestamptz;
CREATE INDEX IF NOT EXISTS users_deletion_due ON users (deletion_due_at) WHERE deletion_due_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_log_user ON audit_log (user_id, id DESC);
