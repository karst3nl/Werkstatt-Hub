CREATE TABLE wartung_settings (
    id INTEGER PRIMARY KEY CHECK(id=1),
    warning_days INTEGER NOT NULL CHECK(warning_days BETWEEN 0 AND 365),
    version INTEGER NOT NULL DEFAULT 1 CHECK(version>0)
);
INSERT INTO wartung_settings(id,warning_days,version) VALUES(1,7,1);

CREATE TABLE wartung_objects (
    id INTEGER PRIMARY KEY,
    author_id INTEGER NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK(length(description)<=20000),
    category TEXT NOT NULL CHECK(length(trim(category)) BETWEEN 1 AND 100),
    location TEXT NOT NULL DEFAULT '' CHECK(length(location)<=200),
    interval_value INTEGER NOT NULL CHECK(interval_value BETWEEN 1 AND 1000),
    interval_unit TEXT NOT NULL CHECK(interval_unit IN ('tage','wochen','monate','jahre')),
    baseline_date TEXT NOT NULL CHECK(length(baseline_date)=10),
    warning_days INTEGER CHECK(warning_days BETWEEN 0 AND 365),
    instructions TEXT NOT NULL DEFAULT '' CHECK(length(instructions)<=20000),
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK(version>0)
);
CREATE INDEX wartung_objects_category_active ON wartung_objects(category,active,id);
CREATE TRIGGER wartung_objects_immutable_origin BEFORE UPDATE OF author_id,baseline_date,signature_name,signature_at ON wartung_objects
WHEN NEW.author_id IS NOT OLD.author_id OR NEW.baseline_date IS NOT OLD.baseline_date
 OR NEW.signature_name IS NOT OLD.signature_name OR NEW.signature_at IS NOT OLD.signature_at BEGIN
    SELECT RAISE(ABORT, 'maintenance origin is immutable');
END;
CREATE TRIGGER wartung_objects_no_delete BEFORE DELETE ON wartung_objects BEGIN
    SELECT RAISE(ABORT, 'maintenance objects can only be deactivated');
END;

CREATE TABLE wartung_records (
    id INTEGER PRIMARY KEY,
    object_id INTEGER NOT NULL REFERENCES wartung_objects(id),
    author_id INTEGER NOT NULL REFERENCES users(id),
    performed_date TEXT NOT NULL CHECK(length(performed_date)=10),
    note TEXT NOT NULL DEFAULT '' CHECK(length(note)<=20000),
    finding TEXT NOT NULL DEFAULT '' CHECK(finding IN ('','in_ordnung','mangel')),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
CREATE INDEX wartung_records_object_date ON wartung_records(object_id,performed_date DESC,id DESC);
CREATE TRIGGER wartung_records_no_update BEFORE UPDATE ON wartung_records BEGIN
    SELECT RAISE(ABORT, 'maintenance history is append-only');
END;
CREATE TRIGGER wartung_records_no_delete BEFORE DELETE ON wartung_records BEGIN
    SELECT RAISE(ABORT, 'maintenance history is append-only');
END;

CREATE TABLE wartung_approvals (
    record_id INTEGER PRIMARY KEY REFERENCES wartung_records(id),
    user_id INTEGER NOT NULL REFERENCES users(id),
    signature_name TEXT NOT NULL,
    signature_at TEXT NOT NULL
);
CREATE TRIGGER wartung_approvals_no_update BEFORE UPDATE ON wartung_approvals BEGIN
    SELECT RAISE(ABORT, 'maintenance approvals are append-only');
END;
CREATE TRIGGER wartung_approvals_no_delete BEFORE DELETE ON wartung_approvals BEGIN
    SELECT RAISE(ABORT, 'maintenance approvals are append-only');
END;
