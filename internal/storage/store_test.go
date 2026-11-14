package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
)

type fixture struct {
	s                                 *Store
	ctx                               context.Context
	admin, employee, peer, supervisor core.User
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	f := fixture{s: s, ctx: context.Background()}
	f.admin, err = s.BootstrapAdmin(f.ctx, "Test Admin")
	if err != nil {
		t.Fatal(err)
	}
	f.employee = f.user(t, "Test Mitarbeiter", core.RoleEmployee)
	f.peer = f.user(t, "Test Kollege", core.RoleEmployee)
	f.supervisor = f.user(t, "Test Leitung", core.RoleSupervisor)
	return f
}

func (f fixture) user(t *testing.T, name string, role core.Role) core.User {
	t.Helper()
	u, err := f.s.CreateUser(f.ctx, f.admin.ID, name, role)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f fixture) entry(t *testing.T, author int64) core.Entry {
	t.Helper()
	e, err := f.s.CreateEntry(f.ctx, author, core.EntryInput{Title: "Test Eintrag", Body: "Test Inhalt"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f fixture) audit(t *testing.T) []core.AuditEvent {
	t.Helper()
	events, err := f.s.Audit(f.ctx, f.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func assertError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestEntryRights(t *testing.T) {
	for _, tc := range []struct {
		name             string
		role             core.Role
		own, wantAllowed bool
	}{
		{"employee own", core.RoleEmployee, true, true},
		{"employee another author", core.RoleEmployee, false, false},
		{"supervisor another author", core.RoleSupervisor, false, true},
		{"admin another author", core.RoleAdmin, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			actor := f.user(t, "Test Bearbeiter", tc.role)
			author := f.employee.ID
			if tc.own {
				author = actor.ID
			}
			e := f.entry(t, author)
			before := len(f.audit(t))
			err := f.s.UpdateEntry(f.ctx, actor.ID, e.ID, core.EntryUpdate{Title: "Bearbeitet", Body: e.Body, Status: e.Status, Version: e.Version})
			if tc.wantAllowed {
				if err != nil {
					t.Fatal(err)
				}
				updated, err := f.s.Entry(f.ctx, f.admin.ID, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				if updated.Title != "Bearbeitet" || updated.Version != e.Version+1 {
					t.Fatalf("update not persisted: %#v", updated)
				}
				if len(f.audit(t)) != before+1 {
					t.Fatal("successful update must create one audit event")
				}
			} else {
				assertError(t, err, core.ErrForbidden)
				if len(f.audit(t)) != before {
					t.Fatal("rejected update changed audit history")
				}
				stored, err := f.s.Entry(f.ctx, f.admin.ID, e.ID)
				if err != nil || stored.Title != e.Title {
					t.Fatalf("rejected update changed entry: %#v, %v", stored, err)
				}
			}
		})
	}
}

func TestStatusRequiresSupervisorOrAdmin(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	update := core.EntryUpdate{Title: e.Title, Body: e.Body, Status: "erledigt", Version: e.Version}
	assertError(t, f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, update), core.ErrForbidden)
	if err := f.s.UpdateEntry(f.ctx, f.supervisor.ID, e.ID, update); err != nil {
		t.Fatal(err)
	}
	stored, err := f.s.Entry(f.ctx, f.employee.ID, e.ID)
	if err != nil || stored.Status != "erledigt" {
		t.Fatalf("supervisor status change = %#v, %v", stored, err)
	}
}

func TestReadsAndCommentsRequireActiveActor(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	for _, actor := range []core.User{f.employee, f.peer, f.supervisor, f.admin} {
		if _, err := f.s.Entry(f.ctx, actor.ID, e.ID); err != nil {
			t.Fatalf("%s read: %v", actor.Role, err)
		}
		if _, err := f.s.Entries(f.ctx, actor.ID); err != nil {
			t.Fatalf("%s list: %v", actor.Role, err)
		}
		if err := f.s.AddComment(f.ctx, actor.ID, e.ID, "Test Kommentar"); err != nil {
			t.Fatalf("%s comment: %v", actor.Role, err)
		}
	}
	comments, err := f.s.Comments(f.ctx, f.employee.ID, e.ID)
	if err != nil || len(comments) != 4 {
		t.Fatalf("comments = %#v, %v", comments, err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Entries(f.ctx, f.employee.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.Entry(f.ctx, f.employee.ID, e.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.Comments(f.ctx, f.employee.ID, e.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.CreateEntry(f.ctx, f.employee.ID, core.EntryInput{Title: "Test", Body: "Test"})
	assertError(t, err, core.ErrForbidden)
	assertError(t, f.s.AddComment(f.ctx, f.employee.ID, e.ID, "Test"), core.ErrForbidden)
	assertError(t, f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, core.EntryUpdate{Title: e.Title, Body: e.Body, Status: e.Status, Version: e.Version}), core.ErrForbidden)
}

func TestUserAdministrationRightsAndLastAdmin(t *testing.T) {
	f := newFixture(t)
	for _, actor := range []core.User{f.employee, f.supervisor} {
		_, err := f.s.CreateUser(f.ctx, actor.ID, "Nicht erlaubt", core.RoleAdmin)
		assertError(t, err, core.ErrForbidden)
		assertError(t, f.s.UpdateUser(f.ctx, actor.ID, f.peer.ID, "Nicht erlaubt", core.RoleAdmin, true), core.ErrForbidden)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, f.admin.Name, core.RoleAdmin, false); err == nil {
		t.Fatal("last active admin must not be deactivated")
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, f.admin.Name, core.RoleEmployee, true); err == nil {
		t.Fatal("last active admin must not be demoted")
	}
	second := f.user(t, "Zweiter Test Admin", core.RoleAdmin)
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, f.admin.Name, core.RoleEmployee, true); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.CreateUser(f.ctx, f.admin.ID, "Nicht erlaubt", core.RoleEmployee)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.CreateUser(f.ctx, second.ID, "Erlaubt", core.RoleEmployee)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapOnlyOnceAndAudited(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.BootstrapAdmin(ctx, "Test Admin")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.BootstrapAdmin(ctx, "Anderer Admin")
	assertError(t, err, core.ErrAlreadyInitialized)
	events, err := s.Audit(ctx, u.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("bootstrap audit = %#v, %v", events, err)
	}
	if events[0].ActorID != u.ID || events[0].ActorName != u.Name || events[0].EntityID != u.ID || events[0].At.IsZero() {
		t.Fatalf("missing bootstrap attribution: %#v", events[0])
	}
}

func TestImmutableSignaturesAndAuditSnapshots(t *testing.T) {
	f := newFixture(t)
	start := time.Now().Add(-time.Second)
	e := f.entry(t, f.employee.ID)
	if e.Signature.Name != f.employee.Name || e.Signature.At.IsZero() || e.Signature.At.Before(start) || e.AuthorID != f.employee.ID {
		t.Fatalf("entry signature = %#v", e)
	}
	if !e.CreatedAt.Equal(e.Signature.At) {
		t.Fatalf("created at %s differs from signed at %s", e.CreatedAt, e.Signature.At)
	}
	if err := f.s.AddComment(f.ctx, f.employee.ID, e.ID, "Test Kommentar"); err != nil {
		t.Fatal(err)
	}
	comments, err := f.s.Comments(f.ctx, f.admin.ID, e.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments = %#v, %v", comments, err)
	}
	originalComment := comments[0]
	if originalComment.Signature.Name != f.employee.Name || originalComment.Signature.At.IsZero() {
		t.Fatalf("comment signature = %#v", originalComment)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, "Umbenannter Test Nutzer", f.employee.Role, true); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, core.EntryUpdate{Title: "Neuer Titel", Body: "Neuer Inhalt", Status: e.Status, Version: e.Version}); err != nil {
		t.Fatal(err)
	}
	updated, err := f.s.Entry(f.ctx, f.admin.ID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Signature != e.Signature || updated.AuthorID != e.AuthorID || !updated.CreatedAt.Equal(e.CreatedAt) {
		t.Fatalf("signature/author changed: %#v -> %#v", e, updated)
	}
	comments, err = f.s.Comments(f.ctx, f.admin.ID, e.ID)
	if err != nil || comments[0].Signature != originalComment.Signature {
		t.Fatalf("comment signature changed: %#v, %v", comments, err)
	}
	var updateEvent *core.AuditEvent
	for _, event := range f.audit(t) {
		if event.EntityType == "entry" && event.EntityID == e.ID && event.Before != "" && event.Before != "null" {
			copy := event
			updateEvent = &copy
		}
	}
	if updateEvent == nil {
		t.Fatal("entry update audit missing")
	}
	if updateEvent.ActorID != f.employee.ID || updateEvent.ActorName != "Umbenannter Test Nutzer" || updateEvent.At.IsZero() {
		t.Fatalf("audit actor missing: %#v", updateEvent)
	}
	var before, after core.Entry
	if err := json.Unmarshal([]byte(updateEvent.Before), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(updateEvent.After), &after); err != nil {
		t.Fatal(err)
	}
	if before.Title != e.Title || before.Body != e.Body || after.Title != updated.Title || after.Body != updated.Body || before.Version != e.Version || after.Version != updated.Version {
		t.Fatalf("incorrect snapshots: before=%#v, after=%#v", before, after)
	}
	if before.Signature != after.Signature {
		t.Fatal("audit signature changed")
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, "Noch ein Name", f.employee.Role, true); err != nil {
		t.Fatal(err)
	}
	for _, event := range f.audit(t) {
		if event.ID == updateEvent.ID && event.ActorName != "Umbenannter Test Nutzer" {
			t.Fatal("historical audit actor name changed after rename")
		}
	}
}

func TestAuditReadPermissionsAndAppendOnly(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.Audit(f.ctx, f.employee.ID)
	assertError(t, err, core.ErrForbidden)
	if _, err := f.s.Audit(f.ctx, f.supervisor.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, f.supervisor.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Audit(f.ctx, f.supervisor.ID)
	assertError(t, err, core.ErrForbidden)
	count := len(f.audit(t))
	for _, query := range []string{"UPDATE audit_log SET actor_name = 'manipuliert'", "DELETE FROM audit_log"} {
		if _, err := f.s.db.ExecContext(f.ctx, query); err == nil {
			t.Fatalf("audit mutation allowed: %s", query)
		}
	}
	if len(f.audit(t)) != count {
		t.Fatal("audit history changed")
	}
}

func TestMutationRollsBackWhenAuditInsertFails(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	before := len(f.audit(t))
	if _, err := f.s.db.ExecContext(f.ctx, "CREATE TRIGGER test_audit_failure BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'test audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateEntry(f.ctx, f.employee.ID, core.EntryInput{Title: "Darf nicht bleiben", Body: "Test"}); err == nil {
		t.Fatal("create must fail with audit")
	}
	if err := f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, core.EntryUpdate{Title: "Darf nicht bleiben", Body: e.Body, Status: e.Status, Version: e.Version}); err == nil {
		t.Fatal("update must fail with audit")
	}
	if err := f.s.AddComment(f.ctx, f.employee.ID, e.ID, "Darf nicht bleiben"); err == nil {
		t.Fatal("comment must fail with audit")
	}
	if _, err := f.s.CreateUser(f.ctx, f.admin.ID, "Darf nicht bleiben", core.RoleEmployee); err == nil {
		t.Fatal("create user must fail with audit")
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, "Darf nicht bleiben", f.peer.Role, true); err == nil {
		t.Fatal("update user must fail with audit")
	}
	entries, err := f.s.Entries(f.ctx, f.admin.ID)
	if err != nil || len(entries) != 1 || entries[0].Title != e.Title || entries[0].Version != e.Version {
		t.Fatalf("entry rollback failed: %#v, %v", entries, err)
	}
	comments, err := f.s.Comments(f.ctx, f.admin.ID, e.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("comment rollback failed: %#v, %v", comments, err)
	}
	users, err := f.s.Users(f.ctx)
	if err != nil || len(users) != 4 {
		t.Fatalf("user creation rollback failed: %#v, %v", users, err)
	}
	peer, err := f.s.User(f.ctx, f.peer.ID)
	if err != nil || peer.Name != f.peer.Name {
		t.Fatalf("user update rollback failed: %#v, %v", peer, err)
	}
	if len(f.audit(t)) != before {
		t.Fatal("failed mutations changed audit history")
	}
}

func TestStaleEntryUpdateDoesNotOverwrite(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	if err := f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, core.EntryUpdate{Title: "Erste Änderung", Body: e.Body, Status: e.Status, Version: e.Version}); err != nil {
		t.Fatal(err)
	}
	before := len(f.audit(t))
	assertError(t, f.s.UpdateEntry(f.ctx, f.supervisor.ID, e.ID, core.EntryUpdate{Title: "Veraltete Änderung", Body: e.Body, Status: e.Status, Version: e.Version}), core.ErrConflict)
	updated, err := f.s.Entry(f.ctx, f.admin.ID, e.ID)
	if err != nil || updated.Title != "Erste Änderung" {
		t.Fatalf("stale overwrite: %#v, %v", updated, err)
	}
	if len(f.audit(t)) != before {
		t.Fatal("stale update created audit event")
	}
}

func TestMigrationsIdempotentAndForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, "Test Admin")
	if err != nil {
		t.Fatal(err)
	}
	var initial int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&initial); err != nil {
		t.Fatal(err)
	}
	if initial < 1 {
		t.Fatal("no versioned migrations recorded")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var reopened int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&reopened); err != nil {
		t.Fatal(err)
	}
	if initial != reopened {
		t.Fatalf("migration replay changed count %d -> %d", initial, reopened)
	}
	users, err := s.Users(ctx)
	if err != nil || len(users) != 1 || users[0].ID != admin.ID {
		t.Fatalf("reopen lost users: %#v, %v", users, err)
	}
	var foreignKeys int
	if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatal("foreign keys not enabled")
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", admin.ID); err == nil {
		t.Fatal("foreign key allowed deleting audit actor")
	}
}

func TestDatabaseRejectsSignatureMutation(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	if err := f.s.AddComment(f.ctx, f.employee.ID, e.ID, "Test Kommentar"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE entries SET signature_name = 'manipuliert'",
		"UPDATE entries SET signature_at = '1970-01-01T00:00:00Z'",
		"UPDATE entries SET author_id = " + strconv.FormatInt(f.peer.ID, 10),
		"UPDATE entries SET created_at = '1970-01-01T00:00:00Z'",
		"UPDATE comments SET signature_name = 'manipuliert'",
		"UPDATE comments SET signature_at = '1970-01-01T00:00:00Z'",
		"UPDATE comments SET author_id = " + strconv.FormatInt(f.peer.ID, 10),
	} {
		if _, err := f.s.db.ExecContext(f.ctx, query); err == nil {
			t.Fatalf("signature mutation allowed: %s", query)
		}
	}
}

func TestInvalidMutationsLeaveNoAuditOrData(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	before := len(f.audit(t))
	for _, input := range []core.EntryInput{{Title: "", Body: "Text"}, {Title: "Titel", Body: " "}, {Title: " ", Body: " "}} {
		_, err := f.s.CreateEntry(f.ctx, f.employee.ID, input)
		assertError(t, err, core.ErrValidation)
	}
	assertError(t, f.s.AddComment(f.ctx, f.employee.ID, e.ID, "  "), core.ErrValidation)
	assertError(t, f.s.UpdateEntry(f.ctx, f.admin.ID, e.ID, core.EntryUpdate{Title: e.Title, Body: e.Body, Status: "unbekannt", Version: e.Version}), core.ErrValidation)
	_, err := f.s.CreateUser(f.ctx, f.admin.ID, "Test Ungültig", core.Role("owner"))
	assertError(t, err, core.ErrValidation)
	_, err = f.s.CreateUser(f.ctx, f.admin.ID, " ", core.RoleEmployee)
	assertError(t, err, core.ErrValidation)
	if len(f.audit(t)) != before {
		t.Fatal("invalid mutations added audit events")
	}
}

func TestConcurrentUpdatesProduceOneAuditedWinner(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	before := len(f.audit(t))
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, title := range []string{"Änderung A", "Änderung B"} {
		go func(title string) {
			<-start
			results <- f.s.UpdateEntry(f.ctx, f.employee.ID, e.ID, core.EntryUpdate{Title: title, Body: e.Body, Status: e.Status, Version: e.Version})
		}(title)
	}
	close(start)
	var succeeded, conflicted int
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, core.ErrConflict) {
			conflicted++
		} else {
			t.Fatalf("unexpected update error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent outcomes: %d successes, %d conflicts", succeeded, conflicted)
	}
	updated, err := f.s.Entry(f.ctx, f.admin.ID, e.ID)
	if err != nil || updated.Version != e.Version+1 {
		t.Fatalf("unexpected concurrent result: %#v, %v", updated, err)
	}
	if len(f.audit(t)) != before+1 {
		t.Fatal("concurrent updates must produce exactly one audit event")
	}
}

func TestMigrationHistoryMismatchRejected(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"changed checksum", "UPDATE schema_migrations SET checksum = 'tampered'"},
		{"unknown version", "INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(999,'999_unknown.sql','unknown','2026-01-01T00:00:00Z')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.sqlite")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(tc.query); err != nil {
				s.Close()
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			unexpected, err := Open(path)
			if err == nil {
				unexpected.Close()
				t.Fatal("incompatible migration history must fail startup")
			}
		})
	}
}

func TestCreationAuditIncludesSignedSnapshots(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	if err := f.s.AddComment(f.ctx, f.peer.ID, e.ID, "Test Kommentar"); err != nil {
		t.Fatal(err)
	}
	var entryFound, commentFound bool
	for _, event := range f.audit(t) {
		switch {
		case event.EntityType == "entry" && event.EntityID == e.ID:
			entryFound = true
			if event.Before != "null" || event.Action != "entry.create" || event.ActorID != f.employee.ID {
				t.Fatalf("entry creation audit = %#v", event)
			}
			var snapshot core.Entry
			if err := json.Unmarshal([]byte(event.After), &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Signature != e.Signature || snapshot.AuthorID != e.AuthorID || snapshot.Title != e.Title {
				t.Fatalf("entry creation snapshot = %#v", snapshot)
			}
		case event.EntityType == "comment":
			commentFound = true
			if event.Before != "null" || event.Action != "comment.create" || event.ActorID != f.peer.ID || event.ActorName != f.peer.Name {
				t.Fatalf("comment creation audit = %#v", event)
			}
			var snapshot core.Comment
			if err := json.Unmarshal([]byte(event.After), &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.EntryID != e.ID || snapshot.AuthorID != f.peer.ID || snapshot.Signature.Name != f.peer.Name || snapshot.Signature.At.IsZero() || snapshot.Body != "Test Kommentar" {
				t.Fatalf("comment creation snapshot = %#v", snapshot)
			}
		}
	}
	if !entryFound || !commentFound {
		t.Fatalf("missing signed creation events: entry=%v comment=%v", entryFound, commentFound)
	}
}

func TestMemoryDatabase(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, "Test Admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEntry(ctx, admin.ID, core.EntryInput{Title: "Test", Body: "Test"}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(ctx, admin.ID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("memory database entry = %#v, %v", entries, err)
	}
}

func TestUserNamesUniqueIncludingUnicodeCase(t *testing.T) {
	f := newFixture(t)
	original := f.user(t, "  Jörg Test  ", core.RoleEmployee)
	if original.Name != "Jörg Test" {
		t.Fatalf("name not trimmed: %q", original.Name)
	}
	before := len(f.audit(t))
	_, err := f.s.CreateUser(f.ctx, f.admin.ID, "JÖRG TEST", core.RoleEmployee)
	assertError(t, err, core.ErrValidation)
	assertError(t, f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, "jörg test", f.peer.Role, true), core.ErrValidation)
	if len(f.audit(t)) != before {
		t.Fatal("duplicate names created audit event")
	}
}
