-- Retire the operator MFA feature and erase stored authenticator secrets.
ALTER TABLE users
  DROP COLUMN totp_secret_enc,
  DROP COLUMN totp_enabled,
  DROP COLUMN recovery_hashes;
