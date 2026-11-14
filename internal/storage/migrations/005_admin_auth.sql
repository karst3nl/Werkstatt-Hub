ALTER TABLE users ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1));
ALTER TABLE users ADD COLUMN auth_version INTEGER NOT NULL DEFAULT 1 CHECK(auth_version > 0);
DROP INDEX users_unique_name;
CREATE UNIQUE INDEX users_unique_name ON users(lower(trim(name))) WHERE deleted=0;

CREATE TABLE admin_credentials (
    user_id INTEGER PRIMARY KEY REFERENCES users(id),
    password_hash TEXT NOT NULL,
    must_change INTEGER NOT NULL DEFAULT 1 CHECK(must_change IN (0,1))
);
