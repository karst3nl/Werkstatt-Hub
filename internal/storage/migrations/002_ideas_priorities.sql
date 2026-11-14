-- Rebuild the shared tables together so foreign keys remain enabled throughout.
CREATE TABLE entries_v2 (
    id INTEGER PRIMARY KEY,
    module TEXT NOT NULL DEFAULT 'basis' CHECK(module IN ('basis','idee')),
    author_id INTEGER NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    status TEXT NOT NULL DEFAULT 'neu' CHECK(
        (module = 'basis' AND status IN ('neu','in_pruefung','geplant','erledigt','abgelehnt'))
        OR (module = 'idee' AND status IN ('neu','in_pruefung','geplant','umgesetzt','abgelehnt'))
    ),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0),
    employee_priority INTEGER CHECK(employee_priority BETWEEN 1 AND 100),
    supervisor_priority INTEGER CHECK(supervisor_priority BETWEEN 1 AND 100)
);
INSERT INTO entries_v2 SELECT * FROM entries;

CREATE TABLE comments_v2 (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries_v2(id),
    author_id INTEGER NOT NULL REFERENCES users(id),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
INSERT INTO comments_v2 SELECT * FROM comments;
DROP TABLE comments;
DROP TABLE entries;
ALTER TABLE entries_v2 RENAME TO entries;
ALTER TABLE comments_v2 RENAME TO comments;
CREATE INDEX entries_created ON entries(created_at DESC,id DESC);
CREATE INDEX entries_module_priority ON entries(module,employee_priority DESC,supervisor_priority DESC,id DESC);
CREATE INDEX comments_entry ON comments(entry_id,id);

CREATE TRIGGER entries_immutable_signature BEFORE UPDATE OF module,author_id,signature_name,signature_at,created_at ON entries
WHEN NEW.module IS NOT OLD.module OR NEW.author_id IS NOT OLD.author_id OR NEW.signature_name IS NOT OLD.signature_name
 OR NEW.signature_at IS NOT OLD.signature_at OR NEW.created_at IS NOT OLD.created_at BEGIN
    SELECT RAISE(ABORT, 'entry signature and module are immutable');
END;
CREATE TRIGGER comments_immutable_signature BEFORE UPDATE OF entry_id,author_id,signature_name,signature_at ON comments
WHEN NEW.entry_id IS NOT OLD.entry_id OR NEW.author_id IS NOT OLD.author_id OR NEW.signature_name IS NOT OLD.signature_name
 OR NEW.signature_at IS NOT OLD.signature_at BEGIN
    SELECT RAISE(ABORT, 'comment signature is immutable');
END;

CREATE TABLE ideas (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id),
    category TEXT NOT NULL CHECK(length(trim(category)) BETWEEN 1 AND 100)
);
CREATE INDEX ideas_category ON ideas(category,entry_id);
CREATE TRIGGER ideas_require_module BEFORE INSERT ON ideas
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'idee' BEGIN
    SELECT RAISE(ABORT, 'ideas require an idea entry');
END;
CREATE TRIGGER ideas_immutable_entry BEFORE UPDATE OF entry_id ON ideas
WHEN NEW.entry_id IS NOT OLD.entry_id BEGIN
    SELECT RAISE(ABORT, 'idea entry is immutable');
END;
