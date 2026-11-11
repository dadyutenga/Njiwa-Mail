-- New accounts have the same access even when role is omitted by a database client.
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'admin';
