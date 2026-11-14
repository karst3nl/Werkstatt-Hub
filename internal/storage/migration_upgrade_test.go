package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"werkstatt-hub/internal/ideas"
)

// Build an actual version-1 database rather than recreating legacy rows through
// the current API. This catches table-rebuild bugs that erase comments, rewrite
// signatures, or break their foreign keys while adding the ideas module.
func TestUpgradeFromMilestoneOnePreservesDataAndGuards(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	legacy, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = legacy.Close() })
	body, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(body)
	if _, err := legacy.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(1,?,?,?)", "001_initial.sql", hex.EncodeToString(checksum[:]), "2026-11-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	const signed = "2026-11-02T08:12:03.123456789Z"
	const updated = "2026-11-03T09:14:05.987654321Z"
	const beforeJSON = `{ "id":17, "title":"Vorher", "signature_name":"Historischer Testname" }`
	const afterJSON = `{ "id":17, "title":"Bestandseintrag", "signature_name":"Historischer Testname" }`
	for _, statement := range []string{
		"INSERT INTO users(id,name,role,active) VALUES(1,'Aktueller Testadmin','admin',1)",
		"INSERT INTO users(id,name,role,active) VALUES(2,'Deaktivierter Testmitarbeiter','mitarbeiter',0)",
	} {
		if _, err := legacy.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := legacy.ExecContext(ctx, `INSERT INTO entries(
		id,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority
	) VALUES(17,2,'Bestandseintrag','Historischer Inhalt','erledigt','Historischer Testname',?,?,?,4,4,2)`, signed, signed, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `INSERT INTO comments(id,entry_id,author_id,body,signature_name,signature_at)
		VALUES(29,17,1,'Historischer Kommentar','Früherer Testadmin',?)`, signed); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `INSERT INTO audit_log(id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json)
		VALUES(43,2,'Historischer Testname','entry.update','entry',17,?,?,?)`, updated, beforeJSON, afterJSON); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}
	defer store.Close()
	verify := func() {
		t.Helper()
		e, err := store.Entry(ctx, 1, 17)
		if err != nil {
			t.Fatal(err)
		}
		signedAt, _ := time.Parse(time.RFC3339Nano, signed)
		updatedAt, _ := time.Parse(time.RFC3339Nano, updated)
		if e.ID != 17 || e.AuthorID != 2 || e.Module != "basis" || e.Title != "Bestandseintrag" || e.Body != "Historischer Inhalt" || e.Status != "erledigt" || e.Version != 4 || e.Signature.Name != "Historischer Testname" || !e.Signature.At.Equal(signedAt) || !e.CreatedAt.Equal(signedAt) || !e.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("legacy entry changed during upgrade: %#v", e)
		}
		if e.EmployeePriority == nil || *e.EmployeePriority != 4 || e.SupervisorPriority == nil || *e.SupervisorPriority != 2 {
			t.Fatalf("legacy priorities changed: %#v", e)
		}
		comments, err := store.Comments(ctx, 1, 17)
		if err != nil || len(comments) != 1 {
			t.Fatalf("legacy comments lost: %#v, %v", comments, err)
		}
		c := comments[0]
		if c.ID != 29 || c.EntryID != 17 || c.AuthorID != 1 || c.Body != "Historischer Kommentar" || c.Signature.Name != "Früherer Testadmin" || !c.Signature.At.Equal(signedAt) {
			t.Fatalf("legacy comment changed: %#v", c)
		}
		var actor, action, before, after string
		if err := store.db.QueryRowContext(ctx, "SELECT actor_name,action,before_json,after_json FROM audit_log WHERE id=43").Scan(&actor, &action, &before, &after); err != nil {
			t.Fatal(err)
		}
		if actor != "Historischer Testname" || action != "entry.update" || before != beforeJSON || after != afterJSON {
			t.Fatalf("historical audit rewritten: actor=%q action=%q before=%q after=%q", actor, action, before, after)
		}
		var currentName string
		var active bool
		if err := store.db.QueryRowContext(ctx, "SELECT name,active FROM users WHERE id=2").Scan(&currentName, &active); err != nil || currentName != "Deaktivierter Testmitarbeiter" || active {
			t.Fatalf("legacy user changed: %q active=%v, %v", currentName, active, err)
		}
		rows, err := store.db.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("migration left a foreign-key violation")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	var migrationCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil || migrationCount != 5 {
		t.Fatalf("upgrade history count=%d, %v", migrationCount, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatalf("reopen upgraded database: %v", err)
	}
	defer store.Close()
	verify()
	var repeatedCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&repeatedCount); err != nil || repeatedCount != migrationCount {
		t.Fatalf("migration replay changed history count=%d, %v", repeatedCount, err)
	}
	for _, query := range []string{
		"UPDATE audit_log SET actor_name='Manipuliert' WHERE id=43",
		"DELETE FROM audit_log WHERE id=43",
		"UPDATE entries SET signature_name='Manipuliert' WHERE id=17",
		"UPDATE entries SET author_id=1 WHERE id=17",
		"UPDATE entries SET created_at='1970-01-01T00:00:00Z' WHERE id=17",
		"UPDATE comments SET signature_name='Manipuliert' WHERE id=29",
		"UPDATE comments SET signature_at='1970-01-01T00:00:00Z' WHERE id=29",
		"DELETE FROM entries WHERE id=17",
		"DELETE FROM users WHERE id=2",
	} {
		if _, err := store.db.ExecContext(ctx, query); err == nil {
			t.Fatalf("guard missing after upgrade: %s", query)
		}
	}
	idea, err := store.CreateIdea(ctx, 1, ideas.Input{Title: "Neue Idee nach Upgrade", Body: "Gemeinsame Ablage verbessern", Category: "Ablauf"})
	if err != nil {
		t.Fatal(err)
	}
	if idea.ID <= 17 || idea.Module != "idee" {
		t.Fatalf("new idea reused legacy identifier: %#v", idea)
	}
	if err := store.AddComment(ctx, 1, idea.ID, "Kommentar zur neuen Idee"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM entries WHERE id=?", idea.ID); err == nil {
		t.Fatal("idea/comment foreign keys no longer protect their entry")
	}
	verify()
}
