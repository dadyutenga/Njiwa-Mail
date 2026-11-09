-- The viewer role is removed. Looking at the console is part of admin and owner.
UPDATE users SET role = 'admin' WHERE role = 'viewer';

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('owner','admin'));
