-- Rebuild every entry child with foreign keys enabled and preserve all existing data.
CREATE TABLE entries_v7 (
    id INTEGER PRIMARY KEY,
    module TEXT NOT NULL DEFAULT 'basis' CHECK(module IN ('basis','idee','aufgabe','werkzeugmeldung','anschaffung','materialpruefung')),
    author_id INTEGER NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK(length(trim(title)) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    status TEXT NOT NULL DEFAULT 'neu' CHECK(
        (module = 'basis' AND status IN ('neu','in_pruefung','geplant','erledigt','abgelehnt'))
        OR (module = 'idee' AND status IN ('neu','in_pruefung','geplant','umgesetzt','abgelehnt'))
        OR (module = 'aufgabe' AND status IN ('offen','in_arbeit','zur_kontrolle','erledigt'))
        OR (module = 'werkzeugmeldung' AND status IN ('neu','in_pruefung','erledigt'))
        OR (module = 'anschaffung' AND status IN ('gewuenscht','genehmigt','bestellt','geliefert'))
        OR (module = 'materialpruefung' AND status = 'erfasst')
    ),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0),
    employee_priority INTEGER CHECK(employee_priority BETWEEN 1 AND 100),
    supervisor_priority INTEGER CHECK(supervisor_priority BETWEEN 1 AND 100)
);
INSERT INTO entries_v7 SELECT * FROM entries;

CREATE TABLE comments_v7 (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries_v7(id),
    author_id INTEGER NOT NULL REFERENCES users(id),
    body TEXT NOT NULL CHECK(length(trim(body)) BETWEEN 1 AND 20000),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
INSERT INTO comments_v7 SELECT * FROM comments;
CREATE TABLE ideas_v7 (
    entry_id INTEGER PRIMARY KEY REFERENCES entries_v7(id),
    category TEXT NOT NULL CHECK(length(trim(category)) BETWEEN 1 AND 100)
);
INSERT INTO ideas_v7 SELECT * FROM ideas;
CREATE TABLE tasks_v7 (
    entry_id INTEGER PRIMARY KEY REFERENCES entries_v7(id),
    assignee_id INTEGER NOT NULL REFERENCES users(id),
    due_date TEXT NOT NULL CHECK(length(due_date)=10),
    approved_by INTEGER REFERENCES users(id),
    approved_name TEXT,
    approved_at TEXT,
    CHECK((approved_by IS NULL AND approved_name IS NULL AND approved_at IS NULL)
       OR (approved_by IS NOT NULL AND approved_name IS NOT NULL AND approved_at IS NOT NULL))
);
INSERT INTO tasks_v7 SELECT * FROM tasks;

CREATE TABLE tool_reports_v7 (
    entry_id INTEGER PRIMARY KEY REFERENCES entries_v7(id),
    tool_id INTEGER NOT NULL REFERENCES tools(id),
    observed_condition TEXT NOT NULL CHECK(observed_condition IN ('ok','verschleisst','defekt','ersetzt'))
);
INSERT INTO tool_reports_v7 SELECT * FROM tool_reports;
CREATE TABLE purchases_v7 (
    entry_id INTEGER PRIMARY KEY REFERENCES entries_v7(id),
    tool_id INTEGER REFERENCES tools(id),
    category TEXT NOT NULL CHECK(length(trim(category)) BETWEEN 1 AND 100),
    location TEXT NOT NULL CHECK(length(trim(location))<=100)
);
INSERT INTO purchases_v7 SELECT * FROM purchases;
DROP TABLE purchases;
DROP TABLE tool_reports;
DROP TABLE tasks;
DROP TABLE ideas;
DROP TABLE comments;
DROP TABLE entries;
ALTER TABLE entries_v7 RENAME TO entries;
ALTER TABLE comments_v7 RENAME TO comments;
ALTER TABLE ideas_v7 RENAME TO ideas;
ALTER TABLE tasks_v7 RENAME TO tasks;
ALTER TABLE tool_reports_v7 RENAME TO tool_reports;
ALTER TABLE purchases_v7 RENAME TO purchases;
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

CREATE INDEX tool_reports_tool ON tool_reports(tool_id,entry_id);
CREATE TRIGGER tool_reports_require_module BEFORE INSERT ON tool_reports
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'werkzeugmeldung' BEGIN
    SELECT RAISE(ABORT, 'tool reports require a report entry');
END;
CREATE TRIGGER tool_reports_immutable_ref BEFORE UPDATE OF entry_id,tool_id ON tool_reports
WHEN NEW.entry_id IS NOT OLD.entry_id OR NEW.tool_id IS NOT OLD.tool_id BEGIN
    SELECT RAISE(ABORT, 'report references are immutable');
END;

CREATE INDEX purchases_tool_category ON purchases(tool_id,category,entry_id);
CREATE TRIGGER purchases_require_module BEFORE INSERT ON purchases
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'anschaffung' BEGIN
    SELECT RAISE(ABORT, 'purchases require a purchase entry');
END;
CREATE TRIGGER purchases_immutable_ref BEFORE UPDATE OF entry_id,tool_id ON purchases
WHEN NEW.entry_id IS NOT OLD.entry_id OR NEW.tool_id IS NOT OLD.tool_id BEGIN
    SELECT RAISE(ABORT, 'purchase references are immutable');
END;

-- Preserve the recorded count and note; corrections are new checks or comments.
CREATE TRIGGER material_check_entry_immutable BEFORE UPDATE OF title,body,status ON entries
WHEN OLD.module='materialpruefung' AND (NEW.title IS NOT OLD.title OR NEW.body IS NOT OLD.body OR NEW.status IS NOT OLD.status) BEGIN
    SELECT RAISE(ABORT, 'material checks are immutable');
END;

CREATE TABLE materials (
    id INTEGER PRIMARY KEY,
    author_id INTEGER NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 200),
    unit TEXT NOT NULL CHECK(length(trim(unit)) BETWEEN 1 AND 50),
    location TEXT NOT NULL DEFAULT '' CHECK(length(location)<=200),
    stock INTEGER NOT NULL CHECK(typeof(stock)='integer' AND stock BETWEEN 0 AND 999999999999),
    minimum INTEGER NOT NULL CHECK(typeof(minimum)='integer' AND minimum BETWEEN 0 AND 999999999999),
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version>0)
);
CREATE INDEX materials_active_name ON materials(active,name,id);
CREATE TRIGGER materials_immutable_origin BEFORE UPDATE OF author_id,unit,signature_name,signature_at,created_at ON materials
WHEN NEW.author_id IS NOT OLD.author_id OR NEW.unit IS NOT OLD.unit OR NEW.signature_name IS NOT OLD.signature_name
 OR NEW.signature_at IS NOT OLD.signature_at OR NEW.created_at IS NOT OLD.created_at BEGIN
    SELECT RAISE(ABORT, 'material unit and origin are immutable');
END;
CREATE TRIGGER materials_no_delete BEFORE DELETE ON materials BEGIN
    SELECT RAISE(ABORT, 'materials can only be deactivated');
END;

CREATE TABLE material_checks (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id),
    material_id INTEGER NOT NULL REFERENCES materials(id),
    quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity BETWEEN 0 AND 999999999999)
);
CREATE INDEX material_checks_material ON material_checks(material_id,entry_id);
CREATE TRIGGER material_checks_require_module BEFORE INSERT ON material_checks
WHEN (SELECT module FROM entries WHERE id=NEW.entry_id) IS NOT 'materialpruefung' BEGIN
    SELECT RAISE(ABORT, 'material checks require a material check entry');
END;
CREATE TRIGGER material_checks_no_update BEFORE UPDATE ON material_checks BEGIN
    SELECT RAISE(ABORT, 'material checks are append-only');
END;
CREATE TRIGGER material_checks_no_delete BEFORE DELETE ON material_checks BEGIN
    SELECT RAISE(ABORT, 'material checks are append-only');
END;

CREATE TABLE material_alerts (
    id INTEGER PRIMARY KEY,
    material_id INTEGER NOT NULL REFERENCES materials(id),
    material_name TEXT NOT NULL,
    unit TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity BETWEEN 0 AND 999999999999),
    minimum INTEGER NOT NULL CHECK(typeof(minimum)='integer' AND minimum BETWEEN 0 AND 999999999999),
    author_id INTEGER NOT NULL REFERENCES users(id),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    acknowledged_by INTEGER REFERENCES users(id),
    acknowledged_name TEXT,
    acknowledged_at TEXT,
    CHECK((acknowledged_by IS NULL AND acknowledged_name IS NULL AND acknowledged_at IS NULL)
       OR (acknowledged_by IS NOT NULL AND acknowledged_name IS NOT NULL AND acknowledged_at IS NOT NULL))
);
CREATE UNIQUE INDEX material_alerts_one_open ON material_alerts(material_id) WHERE acknowledged_at IS NULL;
CREATE INDEX material_alerts_material ON material_alerts(material_id,id);
CREATE TRIGGER material_alerts_immutable_origin BEFORE UPDATE ON material_alerts
WHEN NEW.id IS NOT OLD.id OR NEW.material_id IS NOT OLD.material_id OR NEW.material_name IS NOT OLD.material_name
 OR NEW.unit IS NOT OLD.unit OR NEW.quantity IS NOT OLD.quantity OR NEW.minimum IS NOT OLD.minimum
 OR NEW.author_id IS NOT OLD.author_id OR NEW.signature_name IS NOT OLD.signature_name OR NEW.signature_at IS NOT OLD.signature_at
 OR OLD.acknowledged_at IS NOT NULL BEGIN
    SELECT RAISE(ABORT, 'material alert history is immutable');
END;
CREATE TRIGGER material_alerts_no_delete BEFORE DELETE ON material_alerts BEGIN
    SELECT RAISE(ABORT, 'material alert history is append-only');
END;
