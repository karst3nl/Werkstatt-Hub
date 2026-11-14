package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
	"werkstatt-hub/internal/storage"
)

func bootstrapBrowser(t *testing.T) (*storage.Store, *browser, storage.InitialAdmin) {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "initial.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	initial, err := s.EnsureAdminAccess(context.Background())
	if err != nil || len(initial) != 1 {
		t.Fatalf("initialization: %v, count %d", err, len(initial))
	}
	h, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s, newBrowser(t, h), initial[0]
}
func initialLogin(t *testing.T, b *browser, initial storage.InitialAdmin) {
	t.Helper()
	b.request("GET", "/admin/login", nil, false, "")
	w := b.request("POST", "/admin/login", url.Values{"user_id": {strconv.FormatInt(initial.User.ID, 10)}, "password": {initial.Password}}, true, "")
	if w.Code != 303 || w.Header().Get("Location") != "/admin/password" {
		t.Fatalf("initial login: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
}

func TestAdminInitialPasswordMustChangeBeforeProtectedActions(t *testing.T) {
	s, b, initial := bootstrapBrowser(t)
	w := b.request("GET", "/admin/login", nil, false, "")
	if !strings.Contains(w.Body.String(), `name="password"`) {
		t.Fatal("admin login lacks password")
	}
	w = b.request("POST", "/admin/login", url.Values{"user_id": {strconv.FormatInt(initial.User.ID, 10)}, "password": {"Falsches-Passwort"}}, true, "")
	if w.Code != 403 || b.cookies[sessionCookie] != nil {
		t.Fatalf("wrong admin password accepted: %d", w.Code)
	}
	initialLogin(t, b, initial)
	oldSession := b.cookies[sessionCookie].Value
	for _, path := range []string{"/admin/users", "/audit", "/tools/new", "/tasks/new"} {
		w = b.request("GET", path, nil, false, "")
		if w.Code != 303 || w.Header().Get("Location") != "/admin/password" {
			t.Fatalf("pending admin %s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	before, _ := s.Users(context.Background())
	for _, path := range []string{"/admin/users", "/admin/users/1/delete", "/tools/new", "/tasks/new", "/ideas/1/comments", "/ideas/1/priority"} {
		w = b.request("POST", path, url.Values{"name": {"Nicht anlegen"}, "role": {"admin"}, "side": {"vorgesetzter"}, "value": {"5"}, "version": {"1"}}, true, "")
		if w.Code != 303 || w.Header().Get("Location") != "/admin/password" {
			t.Fatalf("pending admin POST %s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	after, _ := s.Users(context.Background())
	if len(before) != len(after) {
		t.Fatal("pending admin created user")
	}
	w = b.request("GET", "/", nil, false, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `href="/admin/users"`) {
		t.Fatal("public dashboard elevated pending admin")
	}
	w = b.request("GET", "/admin/password", nil, false, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Neues Passwort festlegen") {
		t.Fatal("mandatory password form missing")
	}
	form := url.Values{"current_password": {initial.Password}, "new_password": {"Ein-neues-Testpasswort-2026"}, "confirm_password": {"Ein-anderes-Testpasswort"}}
	if w = b.request("POST", "/admin/password", form, false, ""); w.Code != 403 {
		t.Fatalf("password change without CSRF: %d", w.Code)
	}
	if w = b.request("POST", "/admin/password", form, true, ""); w.Code != 400 {
		t.Fatalf("mismatching passwords: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), initial.Password) || strings.Contains(w.Body.String(), form.Get("new_password")) {
		t.Fatal("password reflected in form")
	}
	form.Set("confirm_password", form.Get("new_password"))
	if w = b.request("POST", "/admin/password", form, true, ""); w.Code != 303 || w.Header().Get("Location") != "/admin/users" {
		t.Fatalf("first change: %d %s", w.Code, w.Body)
	}
	if b.cookies[sessionCookie].Value == oldSession {
		t.Fatal("password change did not rotate session")
	}
	if w = b.request("GET", "/admin/users", nil, false, ""); w.Code != 200 {
		t.Fatalf("configured admin blocked: %d %s", w.Code, w.Body)
	}
	replay := newBrowser(t, b.http)
	replay.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: oldSession}
	if w = replay.request("GET", "/admin/users", nil, false, ""); w.Code != 303 || w.Header().Get("Location") != "/admin/login" {
		t.Fatalf("old setup session survived: %d", w.Code)
	}
	events, err := s.Audit(context.Background(), initial.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		payload := e.Before + e.After
		if strings.Contains(payload, initial.Password) || strings.Contains(payload, form.Get("new_password")) || strings.Contains(payload, "pbkdf2") || strings.Contains(payload, "password_hash") {
			t.Fatal("credential leaked into audit")
		}
	}
}

func TestAdminPasswordChangesRevokeOtherSessions(t *testing.T) {
	f := setup(t)
	first := newBrowser(t, f.handler)
	second := newBrowser(t, f.handler)
	first.login(f.admin)
	second.login(f.admin)
	form := url.Values{"current_password": {fixtureAdminPassword}, "new_password": {"Testpasswort-neu-und-lang"}, "confirm_password": {"Testpasswort-neu-und-lang"}}
	w := first.request("POST", "/admin/password", form, true, "")
	if w.Code != 303 {
		t.Fatalf("change password: %d %s", w.Code, w.Body)
	}
	if w = second.request("GET", "/admin/users", nil, false, ""); w.Code != 303 || w.Header().Get("Location") != "/admin/login" {
		t.Fatalf("other admin session survived: %d %s", w.Code, w.Body)
	}
	if w = first.request("GET", "/admin/users", nil, false, ""); w.Code != 200 {
		t.Fatalf("rotated current session lost: %d", w.Code)
	}
}

func TestOnlyPasswordAuthenticatedAdminsManageUsers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, u := range []core.User{f.employee, f.supervisor} {
		b := newBrowser(t, f.handler)
		b.login(u)
		for _, path := range []string{"/admin/users", "/admin/users/2", "/admin/users/2/delete"} {
			w := b.request("POST", path, url.Values{"name": {"Fremd"}, "role": {"admin"}, "temporary_password": {"Ein-neues-Testpasswort"}, "active": {"1"}}, true, "")
			if w.Code != 403 {
				t.Fatalf("%s managed user at %s: %d", u.Role, path, w.Code)
			}
		}
	}
	staff := newBrowser(t, f.handler)
	staff.login(f.employee)
	// A formerly valid name session never becomes an authenticated admin session.
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.employee.ID, f.employee.Name, core.RoleAdmin, true, "Neuer-Admin-Startpasswort"); err != nil {
		t.Fatal(err)
	}
	if w := staff.request("GET", "/admin/users", nil, false, ""); w.Code != 303 {
		t.Fatalf("promoted name session gained admin: %d", w.Code)
	}
	if w := staff.request("POST", "/login", url.Values{"user_id": {strconv.FormatInt(f.employee.ID, 10)}}, true, ""); w.Code != 403 {
		t.Fatalf("admin signed in through names: %d", w.Code)
	}
}

func TestAdminCreatesPromotesAndDeletesUsersWithHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	form := url.Values{"name": {"Weiterer Admin"}, "role": {"admin"}}
	if w := admin.request("POST", "/admin/users", form, true, ""); w.Code != 400 {
		t.Fatalf("admin created without password: %d", w.Code)
	}
	form.Set("temporary_password", "Nur-Test-Startpasswort-2026")
	if w := admin.request("POST", "/admin/users", form, true, ""); w.Code != 303 {
		t.Fatalf("admin creation: %d %s", w.Code, w.Body)
	}
	users, err := f.store.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var created core.User
	for _, u := range users {
		if u.Name == form.Get("name") {
			created = u
		}
	}
	if created.ID == 0 {
		t.Fatal("admin missing")
	}
	newAdmin := newBrowser(t, f.handler)
	newAdmin.request("GET", "/admin/login", nil, false, "")
	w := newAdmin.request("POST", "/admin/login", url.Values{"user_id": {strconv.FormatInt(created.ID, 10)}, "password": {form.Get("temporary_password")}}, true, "")
	if w.Code != 303 || w.Header().Get("Location") != "/admin/password" {
		t.Fatalf("new admin did not require change: %d %q", w.Code, w.Header().Get("Location"))
	}
	idea, err := f.store.CreateIdea(ctx, f.other.ID, ideas.Input{Title: "Bleibende Signatur", Body: "Der Nutzer darf später gelöscht werden.", Category: "Allgemein"})
	if err != nil {
		t.Fatal(err)
	}
	staff := newBrowser(t, f.handler)
	staff.login(f.other)
	target := fmt.Sprintf("/admin/users/%d/delete", f.other.ID)
	if w = admin.request("POST", target, nil, false, ""); w.Code != 403 {
		t.Fatalf("delete without CSRF: %d", w.Code)
	}
	if w = admin.request("POST", target, nil, true, ""); w.Code != 303 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w = staff.request("POST", fmt.Sprintf("/ideas/%d/comments", idea.ID), url.Values{"body": {"Kein Zugriff mehr"}}, true, ""); w.Code != 303 {
		t.Fatalf("deleted user retained session: %d", w.Code)
	}
	if w = admin.request("GET", "/admin/users", nil, false, ""); strings.Contains(w.Body.String(), f.other.Name) {
		t.Fatal("deleted user still in management")
	}
	if w = staff.request("GET", "/ideas/new", nil, false, ""); strings.Contains(w.Body.String(), f.other.Name) {
		t.Fatal("deleted user still selectable")
	}
	preserved, err := f.store.PublicIdea(ctx, idea.ID)
	if err != nil || preserved.Signature != idea.Signature {
		t.Fatalf("deletion modified history: %#v %v", preserved, err)
	}
}

func TestAdminLoginRateLimitsUnknownUsersAsWell(t *testing.T) {
	f := setup(t)
	b := newBrowser(t, f.handler)
	b.request("GET", "/admin/login", nil, false, "")
	for i := 0; i < 5; i++ {
		if w := b.request("POST", "/admin/login", url.Values{"user_id": {"999999"}, "password": {"Falsches Passwort"}}, true, ""); w.Code != 403 {
			t.Fatalf("failure %d: %d", i, w.Code)
		}
	}
	w := b.request("POST", "/admin/login", url.Values{"user_id": {strconv.FormatInt(f.admin.ID, 10)}, "password": {fixtureAdminPassword}}, true, "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || b.cookies[sessionCookie] != nil {
		t.Fatalf("login limit missing: %d %#v", w.Code, w.Header())
	}
}

func TestAdminLoginLimitIsAtomicForConcurrentAttempts(t *testing.T) {
	f := setup(t)
	const requests = 20
	results := make(chan int, requests)
	start := make(chan struct{})
	for i := 0; i < requests; i++ {
		go func() {
			b := newBrowser(t, f.handler)
			b.request("GET", "/admin/login", nil, false, "")
			<-start
			w := b.request("POST", "/admin/login", url.Values{"user_id": {"999999"}, "password": {"Falsches Passwort"}}, true, "")
			results <- w.Code
		}()
	}
	close(start)
	var denied, limited int
	for i := 0; i < requests; i++ {
		switch code := <-results; code {
		case 403:
			denied++
		case 429:
			limited++
		default:
			t.Fatalf("unexpected login status: %d", code)
		}
	}
	if denied != 5 || limited != requests-5 {
		t.Fatalf("admitted %d invalid attempts, limited %d", denied, limited)
	}
}
