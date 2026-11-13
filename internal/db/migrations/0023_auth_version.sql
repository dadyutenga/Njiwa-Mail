-- Sessions are valid only for the credential state in which they were issued.
ALTER TABLE users ADD COLUMN auth_version bigint NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN auth_version bigint NOT NULL DEFAULT 0;
