package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
	"werkstatt-hub/internal/tooling"
)

// These are synthetic test inputs, never credentials for an installed server.
const authTestPassword = "Nur fuer Passworttests 2026!"
const authResetPassword = "Anderes Testpasswort 2026!"

func newAdminAuthStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "admin-auth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func adminHash(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var hash string
	if err := s.db.QueryRow("SELECT password_hash FROM admin_credentials WHERE user_id=?", id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return hash
}
func authAuditCount(t *testing.T, s *Store) int {
	t.Helper()
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM audit_log").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func adminTableCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var count int
	// Table names are fixed test code, not caller input from the application.
	if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func assertAuthSecretsAbsent(t *testing.T, s *Store, secrets ...string) {
	t.Helper()
	rows, err := s.db.Query("SELECT before_json,after_json FROM audit_log")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var before, after string
		if err := rows.Scan(&before, &after); err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			if secret != "" && (strings.Contains(before, secret) || strings.Contains(after, secret)) {
				t.Fatal("audit contains a password or password hash")
			}
		}
		if strings.Contains(before, "password_hash") || strings.Contains(after, "password_hash") {
			t.Fatal("audit exposes credential fields")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAdminAccessBootstrapsOnceAndNeverResets(t *testing.T) {
	s := newAdminAuthStore(t)
	ctx := context.Background()
	initial, err := s.EnsureAdminAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != 1 || initial[0].User.Role != core.RoleAdmin || !initial[0].User.Active || initial[0].User.Deleted || len(initial[0].Password) < 22 {
		t.Fatalf("invalid initial admin result: users=%d", len(initial))
	}
	u, password := initial[0].User, initial[0].Password
	hash := adminHash(t, s, u.ID)
	if hash == password || strings.Contains(hash, password) {
		t.Fatal("database stores the initial password in plaintext")
	}
	state, err := s.AuthenticateAdmin(ctx, u.ID, password)
	if err != nil || !state.MustChange || state.User.ID != u.ID {
		t.Fatalf("initial admin cannot authenticate with mandatory change: %v", err)
	}
	if state.User.AuthVersion < 1 {
		t.Fatal("missing session revocation version")
	}
	body, err := json.Marshal(initial[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), password) || strings.Contains(string(body), hash) {
		t.Fatal("initial admin JSON leaks a credential")
	}
	before := authAuditCount(t, s)
	again, err := s.EnsureAdminAccess(ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("second bootstrap returned a password or failed: count=%d err=%v", len(again), err)
	}
	if adminHash(t, s, u.ID) != hash || authAuditCount(t, s) != before {
		t.Fatal("restart-like bootstrap reset credential or created audit events")
	}
	after, err := s.AdminAccess(ctx, u.ID)
	if err != nil || !after.MustChange || after.User.AuthVersion != state.User.AuthVersion {
		t.Fatalf("bootstrap changed mandatory state: %v", err)
	}
	assertAuthSecretsAbsent(t, s, password, hash)
}

func TestAdminPasswordChangeValidatesAndRevokesOldCredential(t *testing.T) {
	s := newAdminAuthStore(t)
	ctx := context.Background()
	initial, err := s.EnsureAdminAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, password := initial[0].User, initial[0].Password
	hash := adminHash(t, s, u.ID)
	before := authAuditCount(t, s)
	original, err := s.AdminAccess(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAdmin(ctx, u.ID, "Ein falsches Passwort 2026!"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("wrong password: %v", err)
	}
	for _, tc := range []struct {
		current, next string
		want          error
	}{
		{"Ein falsches Passwort 2026!", authTestPassword, core.ErrForbidden},
		{password, "kurz", core.ErrValidation},
		{password, password, core.ErrValidation},
		{password, strings.Repeat("x", 129), core.ErrValidation},
	} {
		_, err := s.ChangeAdminPassword(ctx, u.ID, tc.current, tc.next)
		if !errors.Is(err, tc.want) {
			t.Fatalf("invalid password change: %v, want %v", err, tc.want)
		}
	}
	if adminHash(t, s, u.ID) != hash || authAuditCount(t, s) != before {
		t.Fatal("failed authentication or password validation changed stored state")
	}
	changed, err := s.ChangeAdminPassword(ctx, u.ID, password, authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if changed.MustChange || changed.User.AuthVersion <= original.User.AuthVersion {
		t.Fatal("password change did not clear setup state and revoke old sessions")
	}
	newHash := adminHash(t, s, u.ID)
	if newHash == hash {
		t.Fatal("password hash not replaced")
	}
	if _, err := s.AuthenticateAdmin(ctx, u.ID, password); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("old password remains valid: %v", err)
	}
	current, err := s.AuthenticateAdmin(ctx, u.ID, authTestPassword)
	if err != nil || current.MustChange || current.User.AuthVersion != changed.User.AuthVersion {
		t.Fatalf("new password not usable: %v", err)
	}
	again, err := s.EnsureAdminAccess(ctx)
	if err != nil || len(again) != 0 || adminHash(t, s, u.ID) != newHash {
		t.Fatal("server bootstrap reset a changed password")
	}
	body, err := json.Marshal(current.User)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "password") || strings.Contains(string(body), newHash) || strings.Contains(string(body), "AuthVersion") {
		t.Fatal("User DTO exposes credential internals")
	}
	assertAuthSecretsAbsent(t, s, password, authTestPassword, hash, newHash)
}

func TestLegacyAdminCredentialsAndManagedPromotionReset(t *testing.T) {
	f := newFixture(t)
	initial, err := f.s.EnsureAdminAccess(f.ctx)
	if err != nil || len(initial) != 1 || initial[0].User.ID != f.admin.ID {
		t.Fatalf("legacy admin credential setup: count=%d err=%v", len(initial), err)
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, f.employee.ID, initial[0].Password); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("employee authenticated as admin: %v", err)
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, 999999, initial[0].Password); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("unknown admin authenticated: %v", err)
	}
	if _, err := f.s.CreateUser(f.ctx, f.employee.ID, "Nicht berechtigt", core.RoleAdmin, authTestPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("employee created admin: %v", err)
	}
	assertError(t, f.s.UpdateUser(f.ctx, f.supervisor.ID, f.peer.ID, f.peer.Name, core.RoleAdmin, true, authTestPassword), core.ErrForbidden)
	second, err := f.s.CreateUser(f.ctx, f.admin.ID, "Zweiter Testadmin", core.RoleAdmin, authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.s.AuthenticateAdmin(f.ctx, second.ID, authTestPassword)
	if err != nil || !state.MustChange {
		t.Fatalf("managed admin lacks initial credential/change flag: %v", err)
	}
	before, err := f.s.User(f.ctx, f.peer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, core.RoleAdmin, true, authTestPassword); err != nil {
		t.Fatal(err)
	}
	promoted, err := f.s.AdminAccess(f.ctx, f.peer.ID)
	if err != nil || !promoted.MustChange || promoted.User.AuthVersion <= before.AuthVersion {
		t.Fatalf("promotion did not initialize credential/revoke prior name session: %v", err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, core.RoleEmployee, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, f.peer.ID, authTestPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("demoted admin authenticated: %v", err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, core.RoleAdmin, true, authResetPassword); err != nil {
		t.Fatal(err)
	}
	repromoted, err := f.s.AdminAccess(f.ctx, f.peer.ID)
	if err != nil || repromoted.User.AuthVersion <= promoted.User.AuthVersion {
		t.Fatal("re-promotion could revive an old authenticated session")
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, f.peer.ID, authTestPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("prior promotion password revived: %v", err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, second.ID, second.Name, second.Role, true, authResetPassword); err != nil {
		t.Fatal(err)
	}
	reset, err := f.s.AdminAccess(f.ctx, second.ID)
	if err != nil || !reset.MustChange || reset.User.AuthVersion <= state.User.AuthVersion {
		t.Fatal("admin reset did not revoke previous credential version")
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, second.ID, authTestPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("reset left previous password valid: %v", err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, second.ID, second.Name, second.Role, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, second.ID, authResetPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("inactive admin authenticated: %v", err)
	}
	if _, err := f.s.CreateUser(f.ctx, f.admin.ID, "Normaler Nutzer mit Passwort", core.RoleEmployee, authTestPassword); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("password accepted for ordinary role: %v", err)
	}
	assertAuthSecretsAbsent(t, f.s, initial[0].Password, authTestPassword, authResetPassword, adminHash(t, f.s, f.admin.ID))
}

func TestDeleteUserIsAdminOnlyAndPreservesHistoricalSignatures(t *testing.T) {
	f := newFixture(t)
	entry := f.entry(t, f.employee.ID)
	if err := f.s.AddComment(f.ctx, f.employee.ID, entry.ID, "Historischer Kommentar"); err != nil {
		t.Fatal(err)
	}
	tool, err := f.s.CreateTool(f.ctx, f.admin.ID, tooling.ToolInput{Name: "Historisches Werkzeug", Category: "Prüfung", Location: "Werkstatt", Condition: "ok", AuthorID: f.supervisor.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.s.CreateTask(f.ctx, f.admin.ID, tasks.Input{Title: "Historische Aufgabe", Body: "Prüfschritt", DueDate: "2026-11-14", AssigneeID: f.employee.ID, AuthorID: f.admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.ChangeTaskStatus(f.ctx, f.employee.ID, task.ID, tasks.StatusChange{Status: "zur_kontrolle", Version: task.Version}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ChangeTaskStatus(f.ctx, f.supervisor.ID, task.ID, tasks.StatusChange{Status: "erledigt", Version: task.Version + 1}); err != nil {
		t.Fatal(err)
	}
	approved, err := f.s.Task(f.ctx, f.admin.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := authAuditCount(t, f.s)
	for _, actor := range []int64{f.employee.ID, f.supervisor.ID, 999999} {
		assertError(t, f.s.DeleteUser(f.ctx, actor, f.peer.ID), core.ErrForbidden)
	}
	if authAuditCount(t, f.s) != count {
		t.Fatal("rejected deletion wrote audit")
	}
	before, err := f.s.User(f.ctx, f.employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteUser(f.ctx, f.admin.ID, f.employee.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteUser(f.ctx, f.admin.ID, f.supervisor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.User(f.ctx, f.employee.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleted user remains publicly addressable: %v", err)
	}
	stored, err := userFrom(f.ctx, f.s.db, f.employee.ID)
	if err != nil || !stored.Deleted || stored.Active || stored.AuthVersion <= before.AuthVersion {
		t.Fatalf("soft deletion state invalid: %#v, %v", stored, err)
	}
	lists := []func() ([]core.User, error){func() ([]core.User, error) { return f.s.Users(f.ctx) }, func() ([]core.User, error) { return f.s.ActiveAuthors(f.ctx) }, func() ([]core.User, error) { return f.s.TaskAssignees(f.ctx) }}
	for _, list := range lists {
		users, err := list()
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range users {
			if u.ID == f.employee.ID || u.ID == f.supervisor.ID {
				t.Fatal("deleted user remains in an active/user list")
			}
		}
	}
	historyEntry, err := f.s.Entry(f.ctx, f.admin.ID, entry.ID)
	if err != nil || historyEntry.AuthorID != f.employee.ID || historyEntry.Signature != entry.Signature {
		t.Fatalf("historical entry changed: %#v %v", historyEntry, err)
	}
	comments, err := f.s.Comments(f.ctx, f.admin.ID, entry.ID)
	if err != nil || len(comments) != 1 || comments[0].AuthorID != f.employee.ID || comments[0].Signature.Name != f.employee.Name {
		t.Fatalf("historical comments changed: %#v %v", comments, err)
	}
	historyTask, err := f.s.Task(f.ctx, f.admin.ID, task.ID)
	if err != nil || historyTask.AssigneeID != f.employee.ID || historyTask.Approval == nil || *historyTask.Approval != *approved.Approval {
		t.Fatalf("historical assignment/approval changed: %#v %v", historyTask, err)
	}
	historyTool, err := f.s.Tool(f.ctx, tool.ID)
	if err != nil || historyTool.Signature != tool.Signature {
		t.Fatalf("historical tool signature changed: %#v %v", historyTool, err)
	}
	replacement, err := f.s.CreateUser(f.ctx, f.admin.ID, f.employee.Name, core.RoleEmployee)
	if err != nil || replacement.ID == f.employee.ID {
		t.Fatalf("deleted name cannot be reused with a distinct identity: %#v %v", replacement, err)
	}
	_, err = f.s.CreateEntry(f.ctx, f.employee.ID, core.EntryInput{Title: "Nicht speichern", Body: "Gelöschter Autor"})
	assertError(t, err, core.ErrForbidden)
	assertError(t, f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, f.employee.Name, core.RoleEmployee, true), core.ErrNotFound)
	assertError(t, f.s.DeleteUser(f.ctx, f.admin.ID, f.admin.ID), core.ErrValidation)
	assertError(t, f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, f.admin.Name, core.RoleEmployee, true), core.ErrValidation)
	assertError(t, f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, f.admin.Name, core.RoleAdmin, false), core.ErrValidation)
}

func TestDeleteAdminRemovesCredentialsAndProtectsLastActiveAdmin(t *testing.T) {
	f := newFixture(t)
	second, err := f.s.CreateUser(f.ctx, f.admin.ID, "Zu löschender Admin", core.RoleAdmin, authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteUser(f.ctx, f.admin.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	var credentials int
	if err := f.s.db.QueryRow("SELECT count(*) FROM admin_credentials WHERE user_id=?", second.ID).Scan(&credentials); err != nil || credentials != 0 {
		t.Fatalf("deleted admin credential remains: %d %v", credentials, err)
	}
	if _, err := f.s.AuthenticateAdmin(f.ctx, second.ID, authTestPassword); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("deleted admin authenticated: %v", err)
	}
	assertError(t, f.s.DeleteUser(f.ctx, f.admin.ID, f.admin.ID), core.ErrValidation)
	before := authAuditCount(t, f.s)
	assertError(t, f.s.DeleteUser(f.ctx, f.admin.ID, second.ID), core.ErrNotFound)
	if authAuditCount(t, f.s) != before {
		t.Fatal("repeated deletion changed audit")
	}
}

func TestAdminAuthAndUserMutationsRollbackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	t.Run("empty bootstrap", func(t *testing.T) {
		s := newAdminAuthStore(t)
		if _, err := s.db.Exec("CREATE TRIGGER deny_auth_audit BEFORE INSERT ON audit_log WHEN NEW.action='admin.password_initialize' BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.EnsureAdminAccess(ctx); err == nil {
			t.Fatal("bootstrap survived audit failure")
		}
		if adminTableCount(t, s, "users") != 0 || adminTableCount(t, s, "admin_credentials") != 0 {
			t.Fatal("failed bootstrap left user or credential behind")
		}
	})
	t.Run("existing users", func(t *testing.T) {
		f := newFixture(t)
		initial, err := f.s.EnsureAdminAccess(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		adminState, err := f.s.AdminAccess(f.ctx, f.admin.ID)
		if err != nil {
			t.Fatal(err)
		}
		secondary, err := f.s.CreateUser(f.ctx, f.admin.ID, "Rollback Testadmin", core.RoleAdmin, authResetPassword)
		if err != nil {
			t.Fatal(err)
		}
		secondaryHash := adminHash(t, f.s, secondary.ID)
		hash := adminHash(t, f.s, f.admin.ID)
		count := authAuditCount(t, f.s)
		userCount := adminTableCount(t, f.s, "users")
		peer, err := f.s.User(f.ctx, f.peer.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.db.Exec("CREATE TRIGGER deny_auth_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.ChangeAdminPassword(f.ctx, f.admin.ID, initial[0].Password, authTestPassword); err == nil {
			t.Fatal("password change survived audit failure")
		}
		if err := f.s.DeleteUser(f.ctx, f.admin.ID, f.employee.ID); err == nil {
			t.Fatal("user deletion survived audit failure")
		}
		if err := f.s.DeleteUser(f.ctx, f.admin.ID, secondary.ID); err == nil {
			t.Fatal("admin deletion survived audit failure")
		}
		if adminHash(t, f.s, secondary.ID) != secondaryHash {
			t.Fatal("failed deletion removed the admin credential")
		}
		if _, err := f.s.CreateUser(f.ctx, f.admin.ID, "Nicht speichern", core.RoleAdmin, authTestPassword); err == nil {
			t.Fatal("admin creation survived audit failure")
		}
		if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, core.RoleAdmin, true, authTestPassword); err == nil {
			t.Fatal("admin promotion survived audit failure")
		}
		current, err := f.s.AdminAccess(f.ctx, f.admin.ID)
		if err != nil || current.MustChange != adminState.MustChange || current.User.AuthVersion != adminState.User.AuthVersion || adminHash(t, f.s, f.admin.ID) != hash {
			t.Fatal("password rollback failed")
		}
		existing, err := f.s.User(f.ctx, f.employee.ID)
		if err != nil || !existing.Active || existing.Deleted {
			t.Fatal("user deletion rollback failed")
		}
		stillPeer, err := f.s.User(f.ctx, f.peer.ID)
		if err != nil || stillPeer.Role != peer.Role || stillPeer.AuthVersion != peer.AuthVersion {
			t.Fatal("admin promotion rollback failed")
		}
		if adminTableCount(t, f.s, "users") != userCount || adminTableCount(t, f.s, "admin_credentials") != 2 || authAuditCount(t, f.s) != count {
			t.Fatal("failed changes left partial user, credential, or audit rows")
		}
	})
}

func TestAdminReactivationRequiresMissingCredentialAndPreservesExistingPassword(t *testing.T) {
	f := newFixture(t)
	legacy, err := f.s.CreateUser(f.ctx, f.admin.ID, "Inaktiver Legacy-Testadmin", core.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, legacy.ID, legacy.Name, legacy.Role, false); err != nil {
		t.Fatal(err)
	}
	initial, err := f.s.EnsureAdminAccess(f.ctx)
	if err != nil || len(initial) != 1 || initial[0].User.ID != f.admin.ID {
		t.Fatalf("bootstrap must skip inactive legacy admin: count=%d err=%v", len(initial), err)
	}
	before, err := f.s.User(f.ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	auditBefore := authAuditCount(t, f.s)
	assertError(t, f.s.UpdateUser(f.ctx, f.admin.ID, legacy.ID, legacy.Name, legacy.Role, true), core.ErrValidation)
	unchanged, err := f.s.User(f.ctx, legacy.ID)
	if err != nil || unchanged.Active || unchanged.AuthVersion != before.AuthVersion || authAuditCount(t, f.s) != auditBefore {
		t.Fatalf("rejected reactivation changed profile/version/audit: %#v %v", unchanged, err)
	}
	var credentials int
	if err := f.s.db.QueryRow("SELECT count(*) FROM admin_credentials WHERE user_id=?", legacy.ID).Scan(&credentials); err != nil || credentials != 0 {
		t.Fatalf("inactive legacy admin was unexpectedly provisioned: %d %v", credentials, err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, legacy.ID, legacy.Name, legacy.Role, true, authTestPassword); err != nil {
		t.Fatal(err)
	}
	enabled, err := f.s.AuthenticateAdmin(f.ctx, legacy.ID, authTestPassword)
	if err != nil || !enabled.MustChange || enabled.User.AuthVersion <= before.AuthVersion {
		t.Fatalf("reactivation with temporary password failed: %#v %v", enabled, err)
	}
	changed, err := f.s.ChangeAdminPassword(f.ctx, legacy.ID, authTestPassword, authResetPassword)
	if err != nil {
		t.Fatal(err)
	}
	hash := adminHash(t, f.s, legacy.ID)
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, legacy.ID, legacy.Name, legacy.Role, false); err != nil {
		t.Fatal(err)
	}
	inactive, err := f.s.User(f.ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inactive.AuthVersion <= changed.User.AuthVersion {
		t.Fatal("deactivation did not revoke existing sessions")
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, legacy.ID, legacy.Name, legacy.Role, true); err != nil {
		t.Fatal(err)
	}
	reactivated, err := f.s.AuthenticateAdmin(f.ctx, legacy.ID, authResetPassword)
	if err != nil || reactivated.MustChange || reactivated.User.AuthVersion <= inactive.AuthVersion || adminHash(t, f.s, legacy.ID) != hash {
		t.Fatalf("reactivation must preserve configured password and revoke previous sessions: %#v %v", reactivated, err)
	}
}
