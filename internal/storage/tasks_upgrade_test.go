package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"werkstatt-hub/internal/tasks"
)

func taskUpgradeSnapshot(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any)
	for _, table := range []string{"users", "entries", "comments", "audit_log", "ideas"} {
		snapshotSelection := "*"
		if table == "users" {
			snapshotSelection = "id,name,role,active"
		}
		rows, err := db.Query("SELECT " + snapshotSelection + " FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		values := make([][]any, 0)
		for rows.Next() {
			row := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range row {
				dest[i] = &row[i]
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		result[table] = values
	}
	return result
}

// Version 3 must rebuild every child of entries without changing any existing
// IDs, signed names/times or serialized audit bytes from either earlier module.
func TestTaskUpgradePreservesMilestoneTwoDataAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "milestone-two.sqlite")
	legacy, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = legacy.Close() })
	if _, err := legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version, name := range []string{"001_initial.sql", "002_ideas_priorities.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.Exec(string(body)); err != nil {
			t.Fatalf("legacy %s: %v", name, err)
		}
		checksum := sha256.Sum256(body)
		if _, err := legacy.Exec("INSERT INTO schema_migrations VALUES(?,?,?,?)", version+1, name, hex.EncodeToString(checksum[:]), "2026-11-01T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	statements := []string{
		`INSERT INTO users(id,name,role,active) VALUES(1,'Aktueller Testadmin','admin',1),(2,'Testmitarbeiter','mitarbeiter',1),(3,'Aktuelle Testleitung','vorgesetzter',1)`,
		`INSERT INTO entries(id,module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority)
   VALUES(17,'basis',2,'Historischer Basiseintrag','Bisheriger Inhalt','erledigt','Früherer Testmitarbeiter','2026-11-02T08:12:03.123456789Z','2026-11-02T08:12:03.123456789Z','2026-11-03T09:14:05.987654321Z',4,4,2)`,
		`INSERT INTO entries(id,module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority)
   VALUES(31,'idee',3,'Historische Idee','Idee vor dem Aufgabenboard','umgesetzt','Frühere Testleitung','2026-11-04T11:22:33.123456789Z','2026-11-04T11:22:33.123456789Z','2026-11-05T12:23:34.123456789Z',7,5,1)`,
		`INSERT INTO ideas(entry_id,category) VALUES(31,'Arbeitsablauf')`,
		`INSERT INTO comments(id,entry_id,author_id,body,signature_name,signature_at)
   VALUES(29,17,1,'Historischer Basiskommentar','Früherer Testadmin','2026-11-02T09:12:03.123456789Z'),
   (41,31,2,'Historischer Ideenkommentar','Früherer Testmitarbeiter','2026-11-04T12:22:33.123456789Z')`,
		`INSERT INTO audit_log(id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json)
   VALUES(43,2,'Früherer Testmitarbeiter','entry.update','entry',17,'2026-11-03T09:14:05.987654321Z','{ "title": "Vorher" }','{ "title": "Historischer Basiseintrag" }'),
   (57,3,'Frühere Testleitung','idea.update','idea',31,'2026-11-05T12:23:34.123456789Z','{ "status": "geplant", "category": "Arbeitsablauf" }','{ "status": "umgesetzt", "category": "Arbeitsablauf" }')`,
	}
	for _, statement := range statements {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	before := taskUpgradeSnapshot(t, legacy)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from version 2: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verify := func() {
		t.Helper()
		if after := taskUpgradeSnapshot(t, store.db); !reflect.DeepEqual(after, before) {
			t.Fatalf("legacy SQL values changed:\nbefore=%#v\nafter=%#v", before, after)
		}
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 7 {
			t.Fatalf("migration count=%d, err=%v", count, err)
		}
		rows, err := store.db.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatal("upgrade broke an entries child foreign key")
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen version 3: %v", err)
	}
	verify()
	for _, statement := range []string{
		`UPDATE audit_log SET actor_name='Manipuliert' WHERE id=43`,
		`DELETE FROM audit_log WHERE id=57`,
		`UPDATE entries SET signature_name='Manipuliert' WHERE id=17`,
		`UPDATE entries SET signature_at='1970-01-01T00:00:00Z' WHERE id=31`,
		`UPDATE entries SET author_id=1 WHERE id=31`,
		`UPDATE entries SET module='basis' WHERE id=31`,
		`UPDATE comments SET signature_name='Manipuliert' WHERE id=29`,
		`UPDATE comments SET entry_id=17 WHERE id=41`,
		`UPDATE ideas SET entry_id=17 WHERE entry_id=31`,
		`INSERT INTO ideas(entry_id,category) VALUES(17,'Falsches Modul')`,
		`DELETE FROM entries WHERE id=17`,
		`DELETE FROM entries WHERE id=31`,
	} {
		if _, err := store.db.Exec(statement); err == nil {
			t.Fatalf("guard lost after upgrade: %s", statement)
		}
	}
	verify()
	task, err := store.CreateTask(ctx, 3, tasks.Input{Title: "Aufgabe nach Upgrade", Body: "Werkzeugablage prüfen", DueDate: "2026-11-20", AssigneeID: 2, AuthorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID <= 31 || task.Module != "aufgabe" || task.AuthorID != 1 || task.AssigneeID != 2 {
		t.Fatalf("new task has invalid identity: %#v", task)
	}
	if err := store.AddComment(ctx, 2, task.ID, "Kommentar zur neuen Aufgabe"); err != nil {
		t.Fatal(err)
	}
	comments, err := store.PublicComments(ctx, task.ID)
	if err != nil || len(comments) != 1 || comments[0].EntryID != task.ID {
		t.Fatalf("task comments: %#v %v", comments, err)
	}
	for _, query := range []string{
		fmt.Sprintf("DELETE FROM entries WHERE id=%d", task.ID),
		fmt.Sprintf("UPDATE entries SET signature_name='Manipuliert' WHERE id=%d", task.ID),
		fmt.Sprintf("INSERT INTO ideas(entry_id,category) VALUES(%d,'Falsches Modul')", task.ID),
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("task guard missing: %s", query)
		}
	}
}
