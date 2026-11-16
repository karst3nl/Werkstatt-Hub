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
	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

func materialsUpgradeSnapshot(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any)
	for _, table := range []string{"users", "admin_credentials", "entries", "comments", "ideas", "tasks", "tools", "tool_reports", "purchases", "audit_log", "wartung_settings", "wartung_objects", "wartung_records", "wartung_approvals", "schema_migrations"} {
		query := "SELECT * FROM " + table
		if table == "schema_migrations" {
			query += " WHERE version<=6"
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

// A real version-6 database exercises the table rebuild with every entries
// child populated, including tool reports/purchases added in version 4.
func TestMaterialsUpgradePreservesAllModulesAndIntegrityGuards(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "before-materials.sqlite")
	legacy, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = legacy.Close() })
	if _, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"001_initial.sql", "002_ideas_priorities.sql", "003_tasks.sql", "004_tooling.sql", "005_admin_auth.sql", "006_wartung.sql"} {
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
		 (31,'idee',3,'Historische Idee','Vor den Materialien','umgesetzt','Frühere Testleitung','2026-10-04T11:22:33.123456789Z','2026-10-04T11:22:33.123456789Z','2026-10-05T12:23:34.123456789Z',7,5,1),
		 (47,'aufgabe',3,'Historische Aufgabe','Bereits abgenommen','erledigt','Frühere Testleitung','2026-10-06T06:07:08.123456789Z','2026-10-06T06:07:08.123456789Z','2026-10-07T07:08:09.987654321Z',9,3,4),
		 (53,'werkzeugmeldung',2,'Historische Meldung','Griff defekt','erledigt','Früherer Mitarbeiter','2026-10-08T08:09:10.123456789Z','2026-10-08T08:09:10.123456789Z','2026-10-09T09:10:11.987654321Z',3,2,5),
		 (59,'anschaffung',4,'Historischer Wunsch','Bereits geliefert','geliefert','Früherer Testnutzer','2026-10-10T10:11:12.123456789Z','2026-10-10T10:11:12.123456789Z','2026-10-11T11:12:13.987654321Z',5,4,3)`,
		`INSERT INTO ideas(entry_id,category) VALUES(31,'Arbeitsablauf')`,
		`INSERT INTO tasks(entry_id,assignee_id,due_date,approved_by,approved_name,approved_at) VALUES(47,4,'2026-10-07',3,'Früherer Abnehmer','2026-10-07T07:08:09.987654321Z')`,
		`INSERT INTO tools(id,author_id,name,category,location,condition,signature_name,signature_at,created_at,updated_at,version)
		 VALUES(11,1,'Historisches Werkzeug','Handwerkzeug','Werkbank','ersetzt','Früherer Admin','2026-10-01T01:02:03.123456789Z','2026-10-01T01:02:03.123456789Z','2026-10-09T09:10:11.987654321Z',4)`,
		`INSERT INTO tool_reports(entry_id,tool_id,observed_condition) VALUES(53,11,'defekt')`,
		`INSERT INTO purchases(entry_id,tool_id,category,location) VALUES(59,11,'Handwerkzeug','Werkbank')`,
		`INSERT INTO comments(id,entry_id,author_id,body,signature_name,signature_at) VALUES
		 (61,17,1,'Basiskommentar','Früherer Admin','2026-10-02T09:12:03.123456789Z'),
		 (62,31,4,'Ideenkommentar','Früherer Testnutzer','2026-10-04T12:22:33.123456789Z'),
		 (63,47,3,'Aufgabe geprüft','Frühere Testleitung','2026-10-06T08:09:10.123456789Z'),
		 (64,53,3,'Meldung geprüft','Frühere Testleitung','2026-10-08T09:10:11.123456789Z'),
		 (65,59,2,'Anschaffung geliefert','Früherer Mitarbeiter','2026-10-10T11:12:13.123456789Z')`,
		`INSERT INTO audit_log(id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json) VALUES
		 (73,4,'Früherer Testnutzer','entry.update','entry',17,'2026-10-03T09:14:05.987654321Z','{ "title": "Vorher" }','{ "title": "Historischer Basiseintrag" }'),
		 (75,3,'Früherer Abnehmer','task.approve','task',47,'2026-10-07T07:08:09.987654321Z','{ "status": "zur_kontrolle" }','{ "status": "erledigt" }'),
		 (79,1,'Früherer Admin','user.delete','user',4,'2026-10-12T12:13:14.987654321Z','{ "active": true, "deleted": false }','{ "active": false, "deleted": true }')`,
		`UPDATE wartung_settings SET warning_days=9,version=3 WHERE id=1`,
		`INSERT INTO wartung_objects(id,author_id,name,description,category,location,interval_value,interval_unit,baseline_date,warning_days,instructions,active,signature_name,signature_at,updated_at,version)
		 VALUES(81,1,'Historischer Kompressor','Frühere Beschreibung','Druckluft','Halle 2',2,'monate','2024-01-31',NULL,'Vorhandene Prüfschritte',0,'Früherer Admin','2024-02-01T01:02:03.123456789Z','2026-10-09T09:10:11.987654321Z',5)`,
		`INSERT INTO wartung_records(id,object_id,author_id,performed_date,note,finding,signature_name,signature_at)
		 VALUES(83,81,4,'2026-10-08','Historische Wartung','mangel','Früherer Testnutzer','2026-10-09T09:10:11.987654321Z')`,
		`INSERT INTO wartung_approvals(record_id,user_id,signature_name,signature_at) VALUES(83,3,'Früherer Abnehmer','2026-10-10T10:11:12.123456789Z')`,
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
	before := materialsUpgradeSnapshot(t, legacy)
	var oldGuards []string
	rows, err := legacy.Query("SELECT type || ':' || name FROM sqlite_master WHERE type IN ('index','trigger') AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		oldGuards = append(oldGuards, name)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from version 6: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verify := func() {
		t.Helper()
		if after := materialsUpgradeSnapshot(t, store.db); !reflect.DeepEqual(before, after) {
			t.Fatal("migration 007 or restart changed historical tables, signatures, credentials, approvals or migration history")
		}
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 7 {
			t.Fatalf("migration count=%d, err=%v", count, err)
		}
		for _, guard := range oldGuards {
			if err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type || ':' || name=?", guard).Scan(&count); err != nil || count != 1 {
				t.Fatalf("lost existing index or trigger %s: %v", guard, err)
			}
		}
		var storedHash string
		if err := store.db.QueryRow("SELECT password_hash FROM admin_credentials WHERE user_id=1").Scan(&storedHash); err != nil || storedHash != hash || !auth.VerifyPassword(storedHash, password) {
			t.Fatal("upgrade reset or invalidated configured admin credentials")
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
	for _, table := range []string{"materials", "material_checks", "material_alerts"} {
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("migration introduced sample data in %s: count=%d err=%v", table, count, err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen after migration 007: %v", err)
	}
	verify()
	for _, query := range []string{
		`UPDATE audit_log SET actor_name='Manipuliert' WHERE id=73`, `DELETE FROM audit_log WHERE id=75`,
		`UPDATE entries SET signature_name='Manipuliert' WHERE id=17`, `UPDATE entries SET author_id=1 WHERE id=31`,
		`UPDATE comments SET signature_name='Manipuliert' WHERE id=62`, `UPDATE comments SET entry_id=17 WHERE id=63`,
		`UPDATE ideas SET entry_id=17 WHERE entry_id=31`, `INSERT INTO ideas(entry_id,category) VALUES(17,'Falsch')`,
		`UPDATE tasks SET approved_name='Manipuliert' WHERE entry_id=47`, `UPDATE tasks SET entry_id=17 WHERE entry_id=47`,
		`UPDATE tools SET signature_name='Manipuliert' WHERE id=11`,
		`UPDATE tool_reports SET tool_id=999 WHERE entry_id=53`, `UPDATE purchases SET tool_id=NULL WHERE entry_id=59`,
		`DELETE FROM entries WHERE id=53`, `DELETE FROM entries WHERE id=59`, `DELETE FROM users WHERE id=4`,
		`DELETE FROM wartung_objects WHERE id=81`, `UPDATE wartung_records SET note='Manipuliert' WHERE id=83`,
		`DELETE FROM wartung_approvals WHERE record_id=83`,
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Fatalf("existing integrity guard lost during migration 007: %s", query)
		}
	}
	verify()
	material, err := store.CreateMaterial(ctx, 1, materials.Input{Name: "Scheibenklar nach Upgrade", Unit: "Liter", Location: "Lager", Stock: 2000, Minimum: 1000})
	if err != nil {
		t.Fatal(err)
	}
	check, err := store.RecordMaterialCheck(ctx, 2, material.ID, materials.CheckInput{Quantity: 1000, Note: "Bestand nach Migration geprüft", Version: material.Version})
	if err != nil {
		t.Fatal(err)
	}
	if check.Module != materials.ModuleCheck || check.AuthorID != 2 || check.Signature.Name != "Aktueller Testmitarbeiter" {
		t.Fatal("new material check identity invalid after table rebuild")
	}
	if err = store.AddComment(ctx, 3, check.ID, "Bestellung wird geprüft."); err != nil {
		t.Fatal(err)
	}
	priority := 4
	if err = store.SetPriority(ctx, 2, check.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: &priority, Version: check.Version}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.MaterialCheck(ctx, check.ID)
	if err != nil || updated.EmployeePriority == nil || *updated.EmployeePriority != 4 {
		t.Fatalf("new material priority failed after table rebuild: %v", err)
	}
	comments, err := store.PublicComments(ctx, check.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != "Aktuelle Testleitung" {
		t.Fatalf("new material comment failed after table rebuild: %v", err)
	}
	alerts, err := store.MaterialAlerts(ctx, material.ID)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("new material alert failed after table rebuild: %v", err)
	}
	acknowledged, err := store.AcknowledgeMaterialAlert(ctx, 3, material.ID, alerts[0].ID)
	if err != nil || acknowledged.Acknowledgment == nil || acknowledged.Acknowledgment.UserID != 3 {
		t.Fatalf("new material warning acknowledgment failed after upgrade: %v", err)
	}
	if _, err := store.db.Exec(fmt.Sprintf("UPDATE material_checks SET quantity=2000 WHERE entry_id=%d", check.ID)); err == nil {
		t.Fatal("material check quantity can be overwritten")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen populated material database: %v", err)
	}
	after, err := store.MaterialCheck(ctx, check.ID)
	if err != nil || !reflect.DeepEqual(after, updated) {
		t.Fatal("restart modified material check, priorities or signature")
	}
	afterAlerts, err := store.MaterialAlerts(ctx, material.ID)
	if err != nil || len(afterAlerts) != 1 || !reflect.DeepEqual(afterAlerts[0], acknowledged) {
		t.Fatal("restart modified material warning or acknowledgment")
	}
}
