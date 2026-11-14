package web

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"werkstatt-hub/internal/auth"
	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
	"werkstatt-hub/internal/storage"
)

const fixtureAdminPassword = "Nur-im-Test-Adminzugang-2026"

var fixtureHashOnce sync.Once
var fixtureHash string
var fixtureHashError error

// General page tests start with an already configured administrator. Hashing
// once retains the production codec without paying its setup cost per fixture.
func configureFixtureAdmin(t *testing.T, path string, id int64) {
	t.Helper()
	fixtureHashOnce.Do(func() { fixtureHash, fixtureHashError = auth.HashPassword(fixtureAdminPassword) })
	if fixtureHashError != nil {
		t.Fatal(fixtureHashError)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO admin_credentials(user_id,password_hash,must_change) VALUES(?,?,0)", id, fixtureHash); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	store                              *storage.Store
	handler                            http.Handler
	admin, employee, other, supervisor core.User
}

func setup(t *testing.T) fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	a, err := s.BootstrapAdmin(ctx, "Verwaltung")
	if err != nil {
		t.Fatal(err)
	}
	configureFixtureAdmin(t, path, a.ID)
	create := func(name string, role core.Role) core.User {
		u, err := s.CreateUser(ctx, a.ID, name, role)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	e := create("Mitarbeiter Eins", core.RoleEmployee)
	o := create("Mitarbeiter Zwei", core.RoleEmployee)
	v := create("Werkstattleitung", core.RoleSupervisor)
	h, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{store: s, handler: h, admin: a, employee: e, other: o, supervisor: v}
}

type browser struct {
	t       *testing.T
	http    http.Handler
	cookies map[string]*http.Cookie
}

func newBrowser(t *testing.T, h http.Handler) *browser {
	return &browser{t: t, http: h, cookies: make(map[string]*http.Cookie)}
}

func (b *browser) request(method, path string, form url.Values, csrf bool, origin string) *httptest.ResponseRecorder {
	b.t.Helper()
	if form == nil {
		form = make(url.Values)
	}
	if csrf {
		if c := b.cookies[csrfCookie]; c != nil {
			form.Set("csrf", c.Value)
		}
	}
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://workshop.local"+path, body)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.http.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}

func (b *browser) login(u core.User) {
	b.t.Helper()
	path := "/login"
	if u.Role == core.RoleAdmin {
		path = "/admin/login"
	}
	if w := b.request("GET", path, nil, false, ""); w.Code != 200 {
		b.t.Fatalf("login form: %d %s", w.Code, w.Body)
	}
	form := url.Values{"user_id": {strconv.FormatInt(u.ID, 10)}}
	if u.Role == core.RoleAdmin {
		form.Set("password", fixtureAdminPassword)
	}
	w := b.request("POST", path, form, true, "")
	if w.Code != http.StatusSeeOther {
		b.t.Fatalf("login: %d %s", w.Code, w.Body)
	}
}

func TestNameSelectionAndCSRF(t *testing.T) {
	f := setup(t)
	b := newBrowser(t, f.handler)
	w := b.request("GET", "/", nil, false, "")
	if w.Code != 200 || w.Header().Get("Location") != "" {
		t.Fatalf("unauthenticated dashboard: %d", w.Code)
	}
	w = b.request("GET", "/login", nil, false, "")
	if !strings.Contains(w.Body.String(), f.employee.Name) || strings.Contains(w.Body.String(), ">Verwaltung · Admin</option>") {
		t.Fatal("regular name list contains wrong roles")
	}
	form := url.Values{"user_id": {strconv.FormatInt(f.employee.ID, 10)}}
	if w = b.request("POST", "/login", form, false, ""); w.Code != 403 {
		t.Fatalf("login without csrf: %d", w.Code)
	}
	if w = b.request("POST", "/login", form, true, "http://evil.local"); w.Code != 403 {
		t.Fatalf("cross origin login: %d", w.Code)
	}
	if w = b.request("POST", "/login", form, true, "null"); w.Code != 403 {
		t.Fatalf("opaque origin login: %d", w.Code)
	}
	if w = b.request("POST", "/login", form, true, "http://workshop.local"); w.Code != http.StatusSeeOther {
		t.Fatalf("same origin login: %d", w.Code)
	}
	if w = b.request("POST", "/login", url.Values{"user_id": {strconv.FormatInt(f.admin.ID, 10)}}, true, ""); w.Code != 403 {
		t.Fatalf("admin through regular login: %d", w.Code)
	}
	if w = b.request("POST", "/admin/login", form, true, ""); w.Code != 403 {
		t.Fatalf("employee through admin login: %d", w.Code)
	}
	b.login(f.employee)
	c := b.cookies[sessionCookie]
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || len(c.Value) != 64 {
		t.Fatalf("session cookie flags: %#v", c)
	}
	oldToken := c.Value
	b.login(f.employee)
	if b.cookies[sessionCookie].Value == oldToken {
		t.Fatal("login did not rotate token")
	}
	replay := newBrowser(t, f.handler)
	replay.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: oldToken}
	if w = replay.request("GET", "/audit", nil, false, ""); w.Code != 303 {
		t.Fatalf("old token replay: %d", w.Code)
	}
	if w = b.request("GET", "/logout", nil, false, ""); w.Code != 405 {
		t.Fatalf("logout through GET: %d", w.Code)
	}
	if w = b.request("POST", "/logout", nil, false, ""); w.Code != 403 {
		t.Fatalf("logout without csrf: %d", w.Code)
	}
	if w = b.request("POST", "/logout", nil, true, ""); w.Code != 303 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w = b.request("GET", "/audit", nil, false, ""); w.Code != 303 {
		t.Fatalf("logout session still active: %d", w.Code)
	}
}

func TestHTTPRightsAndLiveUserState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, tc := range []struct {
		user                 core.User
		adminCode, auditCode int
	}{{f.employee, 403, 403}, {f.supervisor, 403, 200}, {f.admin, 200, 200}} {
		t.Run(string(tc.user.Role), func(t *testing.T) {
			b := newBrowser(t, f.handler)
			b.login(tc.user)
			if w := b.request("GET", "/admin/users", nil, false, ""); w.Code != tc.adminCode {
				t.Fatalf("admin users: %d %s", w.Code, w.Body)
			}
			if w := b.request("GET", "/audit", nil, false, ""); w.Code != tc.auditCode {
				t.Fatalf("audit: %d %s", w.Code, w.Body)
			}
			if tc.user.Role != core.RoleAdmin {
				w := b.request("POST", "/admin/users", url.Values{"name": {"Forged Admin"}, "role": {"admin"}}, true, "")
				if w.Code != 403 {
					t.Fatalf("forged create user: %d", w.Code)
				}
			}
		})
	}
	b := newBrowser(t, f.handler)
	b.login(f.supervisor)
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, core.RoleEmployee, true); err != nil {
		t.Fatal(err)
	}
	if w := b.request("GET", "/audit", nil, false, ""); w.Code != 303 {
		t.Fatalf("role change not effective for live session: %d", w.Code)
	}
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, core.RoleEmployee, false); err != nil {
		t.Fatal(err)
	}
	if w := b.request("GET", "/audit", nil, false, ""); w.Code != 303 {
		t.Fatalf("inactive user retains session: %d", w.Code)
	}
}

func TestIdeasIgnoreForgedSignatureAndProtectEdits(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	b := newBrowser(t, f.handler)
	b.login(f.employee)
	body := strings.Repeat("ä", 20000)
	w := b.request("POST", "/ideas/new", url.Values{"title": {"<script>alert(1)</script>"}, "body": {body}, "category": {"Arbeitsablauf"}, "author_id": {strconv.FormatInt(f.employee.ID, 10)}, "role": {"admin"}, "signature_name": {"Admin"}, "signature_at": {"1900-01-01"}, "status": {"erledigt"}}, true, "")
	if w.Code != 303 {
		t.Fatalf("unicode entry: %d %s", w.Code, w.Body)
	}
	entries, err := f.store.Ideas(ctx, f.employee.ID, ideas.Filter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %#v", err, entries)
	}
	e := entries[0]
	if e.Signature.Name != f.employee.Name || e.AuthorID != f.employee.ID || e.Signature.At.Year() < 2020 || e.Status != "neu" {
		t.Fatalf("forged fields trusted: %#v", e)
	}
	path := fmt.Sprintf("/ideas/%d", e.ID)
	w = b.request("GET", path, nil, false, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>alert(1)</script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("entry not escaped: %d", w.Code)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'none'") {
		t.Fatal("missing CSP")
	}
	if policy := w.Header().Get("Referrer-Policy"); policy != "same-origin" {
		t.Fatalf("form origin must survive the referrer policy: %q", policy)
	}
	if w = b.request("GET", "/ideas/new", nil, false, ""); w.Code != 200 {
		t.Fatalf("new entry form: %d", w.Code)
	}
	if w = b.request("GET", path+"/edit", nil, false, ""); w.Code != 200 {
		t.Fatalf("own edit form: %d", w.Code)
	}
	forged := url.Values{"title": {"Geändert"}, "body": {"Beschreibung"}, "category": {"Arbeitsablauf"}, "status": {"umgesetzt"}, "version": {strconv.FormatInt(e.Version, 10)}}
	if w = b.request("POST", path+"/edit", forged, true, ""); w.Code != 403 {
		t.Fatalf("employee status escalation: %d %s", w.Code, w.Body)
	}
	other := newBrowser(t, f.handler)
	other.login(f.other)
	if w = other.request("GET", path+"/edit", nil, false, ""); w.Code != 403 {
		t.Fatalf("foreign edit form: %d", w.Code)
	}
	forged.Set("status", "neu")
	if w = other.request("POST", path+"/edit", forged, true, ""); w.Code != 403 {
		t.Fatalf("foreign edit: %d", w.Code)
	}
	if w = other.request("POST", path+"/comments", url.Values{"body": {"Ein hilfreicher Kommentar"}, "signature_name": {"Admin"}}, true, ""); w.Code != 303 {
		t.Fatalf("comment: %d", w.Code)
	}
	comments, err := f.store.Comments(ctx, f.employee.ID, e.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.other.Name {
		t.Fatalf("comment signature: %v %#v", err, comments)
	}
	forged.Del("status")
	if w = b.request("POST", path+"/edit", forged, true, ""); w.Code != 303 {
		t.Fatalf("own text edit: %d %s", w.Code, w.Body)
	}
	if w = b.request("POST", path+"/edit", forged, true, ""); w.Code != 409 {
		t.Fatalf("stale edit not rejected: %d", w.Code)
	}
}

func TestExpiredSessionAndSecureCookie(t *testing.T) {
	f := setup(t)
	h, err := New(f.store, Options{SecureCookies: true, SessionTTL: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, h)
	b.login(f.employee)
	if !b.cookies[sessionCookie].Secure {
		t.Fatal("Secure option ignored")
	}
	if w := b.request("GET", "/audit", nil, false, ""); w.Code != 303 {
		t.Fatalf("expired session accepted: %d", w.Code)
	}
}
