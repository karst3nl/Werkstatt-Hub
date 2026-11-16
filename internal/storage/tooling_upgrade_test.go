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

	"werkstatt-hub/internal/tooling"
)

func toolingUpgradeSnapshot(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any)
	for _, table := range []string{"users", "entries", "comments", "ideas", "tasks", "audit_log"} {
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
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
		result[table] = values
	}
	return result
}

// The tooling migration rebuilds every table referring to entries. Exercise a
// real version-3 database, including a completed task's historical approval,
// instead of creating legacy rows through the current application API.
func TestToolingUpgradePreservesMilestoneThreeDataAndGuards(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "milestone-three.sqlite")
	legacy, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = legacy.Close() })
	if _, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	expectedChecksums := make(map[int]string)
	expectedNames := make(map[int]string)
	for i, name := range []string{"001_initial.sql", "002_ideas_priorities.sql", "003_tasks.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = legacy.Exec(string(body)); err != nil {
			t.Fatalf("legacy %s: %v", name, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		expectedChecksums[i+1] = checksum
		expectedNames[i+1] = name
		if _, err = legacy.Exec("INSERT INTO schema_migrations VALUES(?,?,?,?)", i+1, name, checksum, "2026-11-01T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO users(id,name,role,active) VALUES(1,'Aktueller Testadmin','admin',1),(2,'Aktueller Testmitarbeiter','mitarbeiter',1),(3,'Aktuelle Testleitung','vorgesetzter',1)`,
		`INSERT INTO entries(id,module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority)
   VALUES(17,'basis',2,'Historischer Basiseintrag','Bisheriger Inhalt','erledigt','Früherer Testmitarbeiter','2026-11-02T08:12:03.123456789Z','2026-11-02T08:12:03.123456789Z','2026-11-03T09:14:05.987654321Z',4,4,2),
   (31,'idee',3,'Historische Idee','Idee vor der Werkzeugverwaltung','umgesetzt','Frühere Testleitung','2026-11-04T11:22:33.123456789Z','2026-11-04T11:22:33.123456789Z','2026-11-05T12:23:34.123456789Z',7,5,1),
   (47,'aufgabe',3,'Historische Aufgabe','Bereits geprüfte Arbeit','erledigt','Frühere Testleitung','2026-11-06T06:07:08.123456789Z','2026-11-06T06:07:08.123456789Z','2026-11-07T07:08:09.987654321Z',9,3,4)`,
		`INSERT INTO ideas(entry_id,category) VALUES(31,'Arbeitsablauf')`,
		`INSERT INTO tasks(entry_id,assignee_id,due_date,approved_by,approved_name,approved_at)
   VALUES(47,2,'2026-11-07',3,'Früherer Abnehmer','2026-11-07T07:08:09.987654321Z')`,
		`INSERT INTO comments(id,entry_id,author_id,body,signature_name,signature_at)
   VALUES(61,17,1,'Historischer Basiskommentar','Früherer Testadmin','2026-11-02T09:12:03.123456789Z'),
   (62,31,2,'Historischer Ideenkommentar','Früherer Testmitarbeiter','2026-11-04T12:22:33.123456789Z'),
   (63,47,3,'Historischer Aufgabenkommentar','Frühere Testleitung','2026-11-06T08:09:10.123456789Z')`,
		`INSERT INTO audit_log(id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json)
   VALUES(73,2,'Früherer Testmitarbeiter','entry.update','entry',17,'2026-11-03T09:14:05.987654321Z','{ "title": "Vorher" }','{ "title": "Historischer Basiseintrag" }'),
   (74,3,'Frühere Testleitung','idea.update','idea',31,'2026-11-05T12:23:34.123456789Z','{ "status": "geplant", "category": "Arbeitsablauf" }','{ "status": "umgesetzt", "category": "Arbeitsablauf" }'),
   (75,3,'Früherer Abnehmer','task.approve','task',47,'2026-11-07T07:08:09.987654321Z','{ "status": "zur_kontrolle", "approval": null }','{ "status": "erledigt", "approval": {"user_id":3,"signature":{"name":"Früherer Abnehmer","at":"2026-11-07T07:08:09.987654321Z"}} }')`,
	} {
		if _, err = legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	before := toolingUpgradeSnapshot(t, legacy)
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from version 3: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verify := func() {
		t.Helper()
		if after := toolingUpgradeSnapshot(t, store.db); !reflect.DeepEqual(before, after) {
			t.Fatalf("legacy SQL values changed:\nbefore=%#v\nafter=%#v", before, after)
		}
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 7 {
			t.Fatalf("migration count=%d, err=%v", count, err)
		}
		for version, checksum := range expectedChecksums {
			var storedName, storedChecksum, appliedAt string
			if err := store.db.QueryRow("SELECT name,checksum,applied_at FROM schema_migrations WHERE version=?", version).Scan(&storedName, &storedChecksum, &appliedAt); err != nil || storedName != expectedNames[version] || storedChecksum != checksum || appliedAt != "2026-11-01T10:00:00Z" {
				t.Fatalf("migration %d history changed: %q/%q/%q, %v", version, storedName, storedChecksum, appliedAt, err)
			}
		}
		for _, name := range []string{"entries_created", "entries_module_priority", "comments_entry", "ideas_category", "tasks_assignee_due"} {
			var count int
			if err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&count); err != nil || count != 1 {
				t.Fatalf("index %s lost during upgrade: count=%d, %v", name, count, err)
			}
		}
		rows, err := store.db.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatal("upgrade broke an entries child foreign key")
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen version 4: %v", err)
	}
	verify()
	for _, query := range []string{
		`UPDATE audit_log SET actor_name='Manipuliert' WHERE id=73`,
		`DELETE FROM audit_log WHERE id=75`,
		`UPDATE entries SET signature_name='Manipuliert' WHERE id=17`,
		`UPDATE entries SET signature_at='1970-01-01T00:00:00Z' WHERE id=31`,
		`UPDATE entries SET author_id=1 WHERE id=47`,
		`UPDATE entries SET module='basis' WHERE id=31`,
		`UPDATE comments SET signature_name='Manipuliert' WHERE id=61`,
		`UPDATE comments SET entry_id=17 WHERE id=62`,
		`UPDATE ideas SET entry_id=17 WHERE entry_id=31`,
		`INSERT INTO ideas(entry_id,category) VALUES(17,'Falsches Modul')`,
		`UPDATE tasks SET entry_id=17 WHERE entry_id=47`,
		`UPDATE tasks SET approved_name='Manipuliert' WHERE entry_id=47`,
		`UPDATE tasks SET approved_at='1970-01-01T00:00:00Z' WHERE entry_id=47`,
		`INSERT INTO tasks(entry_id,assignee_id,due_date) VALUES(17,2,'2026-11-20')`,
		`DELETE FROM entries WHERE id=17`,
		`DELETE FROM entries WHERE id=31`,
		`DELETE FROM entries WHERE id=47`,
		`DELETE FROM users WHERE id=2`,
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("guard lost after upgrade: %s", query)
		}
	}
	verify()
	tool, err := store.CreateTool(ctx, 3, tooling.ToolInput{Name: "Werkzeug nach Upgrade", Category: "Handwerkzeug", Location: "Werkbank", Condition: "ok", AuthorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if tool.ID <= 0 || tool.AuthorID != 1 || tool.Signature.Name != "Aktueller Testadmin" {
		t.Fatalf("new tool identity: %#v", tool)
	}
	report, err := store.CreateToolReport(ctx, 2, tooling.ReportInput{ToolID: tool.ID, Title: "Zustand nach Upgrade", Body: "Der Griff ist beschädigt", ObservedCondition: "defekt"})
	if err != nil {
		t.Fatal(err)
	}
	if report.ID <= 47 || report.Module != "werkzeugmeldung" || report.ToolID != tool.ID || report.Signature.Name != "Aktueller Testmitarbeiter" {
		t.Fatalf("new report identity: %#v", report)
	}
	if err := store.AddComment(ctx, 3, report.ID, "Meldung prüfen"); err != nil {
		t.Fatal(err)
	}
	comments, err := store.PublicComments(ctx, report.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("new report comments: %#v, %v", comments, err)
	}
	for _, query := range []string{
		fmt.Sprintf("DELETE FROM tools WHERE id=%d", tool.ID),
		fmt.Sprintf("DELETE FROM entries WHERE id=%d", report.ID),
		fmt.Sprintf("UPDATE tools SET signature_name='Manipuliert' WHERE id=%d", tool.ID),
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("new tooling guard missing: %s", query)
		}
	}
}
