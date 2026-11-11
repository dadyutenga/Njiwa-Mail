-- One operator role. Existing owners become admins; every signed-in user can do the same work.
UPDATE users SET role = 'admin' WHERE role <> 'admin';

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role = 'admin');
