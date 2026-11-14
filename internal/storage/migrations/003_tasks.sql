-- Rebuild the shared tables together so foreign keys remain enabled throughout.
CREATE TABLE entries_v3 (
    id INTEGER PRIMARY KEY,
    module TEXT NOT NULL DEFAULT 'basis' CHECK(module IN ('basis','idee','aufgabe')),
    author_id INTEGER NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    status TEXT NOT NULL DEFAULT 'neu' CHECK(
        (module = 'basis' AND status IN ('neu','in_pruefung','geplant','erledigt','abgelehnt'))
        OR (module = 'idee' AND status IN ('neu','in_pruefung','geplant','umgesetzt','abgelehnt'))
        OR (module = 'aufgabe' AND status IN ('offen','in_arbeit','zur_kontrolle','erledigt'))
    ),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0),
    employee_priority INTEGER CHECK(employee_priority BETWEEN 1 AND 100),
    supervisor_priority INTEGER CHECK(supervisor_priority BETWEEN 1 AND 100)
);
INSERT INTO entries_v3 SELECT * FROM entries;

CREATE TABLE comments_v3 (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries_v3(id),
    author_id INTEGER NOT NULL REFERENCES users(id),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
INSERT INTO comments_v3 SELECT * FROM comments;
CREATE TABLE ideas_v3 (
    entry_id INTEGER PRIMARY KEY REFERENCES entries_v3(id),
    category TEXT NOT NULL CHECK(length(trim(category)) BETWEEN 1 AND 100)
);
INSERT INTO ideas_v3 SELECT * FROM ideas;
DROP TABLE ideas;
DROP TABLE comments;
DROP TABLE entries;
ALTER TABLE entries_v3 RENAME TO entries;
ALTER TABLE comments_v3 RENAME TO comments;
ALTER TABLE ideas_v3 RENAME TO ideas;
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

CREATE INDEX ideas_category ON ideas(category,entry_id);
CREATE TRIGGER ideas_require_module BEFORE INSERT ON ideas
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'idee' BEGIN
    SELECT RAISE(ABORT, 'ideas require an idea entry');
END;
CREATE TRIGGER ideas_immutable_entry BEFORE UPDATE OF entry_id ON ideas
WHEN NEW.entry_id IS NOT OLD.entry_id BEGIN
    SELECT RAISE(ABORT, 'idea entry is immutable');
END;

CREATE TABLE tasks (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id),
    assignee_id INTEGER NOT NULL REFERENCES users(id),
    due_date TEXT NOT NULL CHECK(length(due_date)=10),
    approved_by INTEGER REFERENCES users(id),
    approved_name TEXT,
    approved_at TEXT,
    CHECK((approved_by IS NULL AND approved_name IS NULL AND approved_at IS NULL)
       OR (approved_by IS NOT NULL AND approved_name IS NOT NULL AND approved_at IS NOT NULL))
);
CREATE INDEX tasks_assignee_due ON tasks(assignee_id,due_date,entry_id);
CREATE TRIGGER tasks_require_module BEFORE INSERT ON tasks
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'aufgabe' BEGIN
    SELECT RAISE(ABORT, 'tasks require a task entry');
END;
CREATE TRIGGER tasks_immutable_entry BEFORE UPDATE OF entry_id ON tasks
WHEN NEW.entry_id IS NOT OLD.entry_id BEGIN
    SELECT RAISE(ABORT, 'task entry is immutable');
END;
CREATE TRIGGER tasks_immutable_approval BEFORE UPDATE OF approved_by,approved_name,approved_at ON tasks
WHEN OLD.approved_at IS NOT NULL AND NEW.approved_at IS NOT NULL
 AND (NEW.approved_by IS NOT OLD.approved_by OR NEW.approved_name IS NOT OLD.approved_name OR NEW.approved_at IS NOT OLD.approved_at) BEGIN
    SELECT RAISE(ABORT, 'task approval is immutable until reopened');
END;
