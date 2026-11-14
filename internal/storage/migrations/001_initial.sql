CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 100),
    role TEXT NOT NULL CHECK(role IN ('mitarbeiter', 'vorgesetzter', 'admin')),
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0, 1))
);
CREATE UNIQUE INDEX users_unique_name ON users(lower(trim(name)));

CREATE TABLE entries (
    id INTEGER PRIMARY KEY,
    module TEXT NOT NULL DEFAULT 'basis' CHECK(module = 'basis'),
    author_id INTEGER NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    status TEXT NOT NULL DEFAULT 'neu' CHECK(status IN ('neu','in_pruefung','geplant','erledigt','abgelehnt')),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0),
    employee_priority INTEGER CHECK(employee_priority BETWEEN 1 AND 5),
    supervisor_priority INTEGER CHECK(supervisor_priority BETWEEN 1 AND 5)
);
CREATE INDEX entries_created ON entries(created_at DESC, id DESC);

CREATE TABLE comments (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries(id),
    author_id INTEGER NOT NULL REFERENCES users(id),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
CREATE INDEX comments_entry ON comments(entry_id, id);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    actor_name TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    at TEXT NOT NULL,
    before_json TEXT NOT NULL CHECK(json_valid(before_json)),
    after_json TEXT NOT NULL CHECK(json_valid(after_json))
);
CREATE INDEX audit_entity ON audit_log(entity_type, entity_id, id);

CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_log BEGIN
    SELECT RAISE(ABORT, 'audit log is append-only');
END;
CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_log BEGIN
    SELECT RAISE(ABORT, 'audit log is append-only');
END;
CREATE TRIGGER entries_immutable_signature BEFORE UPDATE OF author_id,signature_name,signature_at,created_at ON entries
WHEN NEW.author_id IS NOT OLD.author_id OR NEW.signature_name IS NOT OLD.signature_name
 OR NEW.signature_at IS NOT OLD.signature_at OR NEW.created_at IS NOT OLD.created_at BEGIN
    SELECT RAISE(ABORT, 'entry signature is immutable');
END;
CREATE TRIGGER comments_immutable_signature BEFORE UPDATE OF author_id,signature_name,signature_at ON comments
WHEN NEW.author_id IS NOT OLD.author_id OR NEW.signature_name IS NOT OLD.signature_name
 OR NEW.signature_at IS NOT OLD.signature_at BEGIN
    SELECT RAISE(ABORT, 'comment signature is immutable');
END;
