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

	"werkstatt-hub/internal/auth"
	"werkstatt-hub/internal/wartung"
)

// Unlike the earlier upgrade snapshots, version 5 already contains deletion
// flags, session versions and password hashes, all of which must remain intact.
func wartungUpgradeSnapshot(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any)
	for _, table := range []string{"users", "admin_credentials", "entries", "comments", "ideas", "tasks", "tools", "tool_reports", "purchases", "audit_log", "schema_migrations"} {
		query := "SELECT * FROM " + table
		if table == "schema_migrations" {
			query += " WHERE version<=5"
		}
		rows, err := db.Query(query + " ORDER BY 1")
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

func TestWartungUpgradePreservesAllModulesAndAdminCredentials(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "before-wartung.sqlite")
	legacy, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = legacy.Close() })
	if _, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"001_initial.sql", "002_ideas_priorities.sql", "003_tasks.sql", "004_tooling.sql", "005_admin_auth.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = legacy.Exec(string(body)); err != nil {
			t.Fatalf("legacy %s: %v", name, err)
		}
		sum := sha256.Sum256(body)
		if _, err = legacy.Exec("INSERT INTO schema_migrations VALUES(?,?,?,?)", i+1, name, hex.EncodeToString(sum[:]), "2026-11-01T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO users(id,name,role,active,deleted,auth_version) VALUES
		 (1,'Aktueller Testadmin','admin',1,0,7),(2,'Aktueller Testmitarbeiter','mitarbeiter',1,0,2),
		 (3,'Aktuelle Testleitung','vorgesetzter',1,0,4),(4,'Gelöschter Testnutzer','mitarbeiter',0,1,9)`,
		`INSERT INTO entries(id,module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority) VALUES
		 (17,'basis',4,'Historischer Basiseintrag','Alter Inhalt','erledigt','Früherer Testnutzer','2026-10-02T08:12:03.123456789Z','2026-10-02T08:12:03.123456789Z','2026-10-03T09:14:05.987654321Z',4,4,2),
		 (31,'idee',3,'Historische Idee','Vor der Wartung','umgesetzt','Frühere Testleitung','2026-10-04T11:22:33.123456789Z','2026-10-04T11:22:33.123456789Z','2026-10-05T12:23:34.123456789Z',7,5,1),
		 (47,'aufgabe',3,'Historische Aufgabe','Bereits abgenommen','erledigt','Frühere Testleitung','2026-10-06T06:07:08.123456789Z','2026-10-06T06:07:08.123456789Z','2026-10-07T07:08:09.987654321Z',9,3,4),
		 (53,'werkzeugmeldung',2,'Historische Meldung','Griff defekt','erledigt','Früherer Mitarbeiter','2026-10-08T08:09:10.123456789Z','2026-10-08T08:09:10.123456789Z','2026-10-09T09:10:11.987654321Z',3,2,5),
		 (59,'anschaffung',4,'Historischer Wunsch','Bereits geliefert','geliefert','Früherer Testnutzer','2026-10-10T10:11:12.123456789Z','2026-10-10T10:11:12.123456789Z','2026-10-11T11:12:13.987654321Z',5,4,3)`,
		`INSERT INTO ideas(entry_id,category) VALUES(31,'Arbeitsablauf')`,
		`INSERT INTO tasks(entry_id,assignee_id,due_date,approved_by,approved_name,approved_at)
		 VALUES(47,4,'2026-10-07',3,'Früherer Abnehmer','2026-10-07T07:08:09.987654321Z')`,
		`INSERT INTO tools(id,author_id,name,category,location,condition,signature_name,signature_at,created_at,updated_at,version)
		 VALUES(11,1,'Historisches Werkzeug','Handwerkzeug','Werkbank','ersetzt','Früherer Admin','2026-10-01T01:02:03.123456789Z','2026-10-01T01:02:03.123456789Z','2026-10-09T09:10:11.987654321Z',4)`,
		`INSERT INTO tool_reports(entry_id,tool_id,observed_condition) VALUES(53,11,'defekt')`,
		`INSERT INTO purchases(entry_id,tool_id,category,location) VALUES(59,11,'Handwerkzeug','Werkbank')`,
		`INSERT INTO comments(id,entry_id,author_id,body,signature_name,signature_at) VALUES
		 (61,17,1,'Basiskommentar','Früherer Admin','2026-10-02T09:12:03.123456789Z'),
		 (62,31,4,'Ideenkommentar','Früherer Testnutzer','2026-10-04T12:22:33.123456789Z'),
		 (63,53,3,'Meldung geprüft','Frühere Testleitung','2026-10-08T09:10:11.123456789Z')`,
		`INSERT INTO audit_log(id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json) VALUES
		 (73,4,'Früherer Testnutzer','entry.update','entry',17,'2026-10-03T09:14:05.987654321Z','{ "title": "Vorher" }','{ "title": "Historischer Basiseintrag" }'),
		 (75,3,'Früherer Abnehmer','task.approve','task',47,'2026-10-07T07:08:09.987654321Z','{ "status": "zur_kontrolle" }','{ "status": "erledigt" }'),
		 (79,1,'Früherer Admin','user.delete','user',4,'2026-10-12T12:13:14.987654321Z','{ "active": true, "deleted": false }','{ "active": false, "deleted": true }')`,
	} {
		if _, err = legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	password, err := auth.GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec("INSERT INTO admin_credentials(user_id,password_hash,must_change) VALUES(1,?,0)", hash); err != nil {
		t.Fatal(err)
	}
	before := wartungUpgradeSnapshot(t, legacy)
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from version 5: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verify := func() {
		t.Helper()
		if after := wartungUpgradeSnapshot(t, store.db); !reflect.DeepEqual(after, before) {
			t.Fatal("migration 006 or restart changed historical module data, signatures, credentials, user flags or migration history")
		}
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 6 {
			t.Fatalf("migration count=%d, err=%v", count, err)
		}
		var storedHash string
		if err := store.db.QueryRow("SELECT password_hash FROM admin_credentials WHERE user_id=1").Scan(&storedHash); err != nil || storedHash != hash || !auth.VerifyPassword(storedHash, password) {
			t.Fatal("upgrade reset or invalidated the configured admin password")
		}
		rows, err := store.db.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("upgrade left a foreign-key violation")
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen after migration 006: %v", err)
	}
	verify()
	for _, query := range []string{
		`UPDATE audit_log SET actor_name='Manipuliert' WHERE id=73`,
		`DELETE FROM audit_log WHERE id=75`,
		`UPDATE entries SET signature_name='Manipuliert' WHERE id=17`,
		`UPDATE comments SET signature_name='Manipuliert' WHERE id=62`,
		`UPDATE tasks SET approved_name='Manipuliert' WHERE entry_id=47`,
		`UPDATE tools SET signature_name='Manipuliert' WHERE id=11`,
		`UPDATE tool_reports SET tool_id=999 WHERE entry_id=53`,
		`DELETE FROM entries WHERE id=53`,
		`DELETE FROM users WHERE id=4`,
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("existing integrity guard lost after migration 006: %s", query)
		}
	}
	verify()
	object, err := store.CreateWartungObject(ctx, 1, wartung.Input{Name: "Kompressor nach Upgrade", Category: "Druckluft", LastMaintenanceDate: "2020-01-31", IntervalValue: 1, IntervalUnit: "monate"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.RecordWartung(ctx, 2, object.ID, wartung.RecordInput{PerformedDate: "2020-02-29", Note: "Test nach Upgrade", Finding: "in_ordnung"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.ApproveWartung(ctx, 3, object.ID, record.ID)
	if err != nil || approved.Approval == nil || approved.Approval.UserID != 3 {
		t.Fatalf("new maintenance record approval failed after upgrade: %v", err)
	}
	for _, query := range []string{
		fmt.Sprintf("DELETE FROM wartung_objects WHERE id=%d", object.ID),
		fmt.Sprintf("UPDATE wartung_objects SET baseline_date='2021-01-01' WHERE id=%d", object.ID),
		fmt.Sprintf("UPDATE wartung_objects SET signature_name='Manipuliert' WHERE id=%d", object.ID),
		fmt.Sprintf("UPDATE wartung_records SET note='Manipuliert' WHERE id=%d", record.ID),
		fmt.Sprintf("DELETE FROM wartung_records WHERE id=%d", record.ID),
		fmt.Sprintf("UPDATE wartung_approvals SET signature_name='Manipuliert' WHERE record_id=%d", record.ID),
		fmt.Sprintf("DELETE FROM wartung_approvals WHERE record_id=%d", record.ID),
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("new maintenance history guard missing after upgrade: %s", query)
		}
	}
	history, err := store.WartungHistory(ctx, object.ID)
	if err != nil || len(history) != 1 || history[0].Approval == nil {
		t.Fatalf("maintenance history lost after rejected manipulation: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen populated maintenance database: %v", err)
	}
	after, err := store.WartungHistory(ctx, object.ID)
	if err != nil || !reflect.DeepEqual(after, history) {
		t.Fatal("restart modified maintenance history or approval")
	}
}
