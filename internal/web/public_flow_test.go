package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
	"werkstatt-hub/internal/storage"
)

func publicCreatedEntry(t *testing.T, f fixture, module string) core.Entry {
	t.Helper()
	if module == "idee" {
		list, err := f.store.Ideas(context.Background(), f.admin.ID, ideas.Filter{})
		if err != nil || len(list) != 1 {
			t.Fatalf("created ideas: %#v, %v", list, err)
		}
		return list[0].Entry
	}
	list, err := f.store.Entries(context.Background(), f.admin.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("created entries: %#v, %v", list, err)
	}
	return list[0]
}
func publicAudit(t *testing.T, f fixture) []core.AuditEvent {
	t.Helper()
	events, err := f.store.Audit(context.Background(), f.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestPublicDashboardAndCreationForms(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	entry, err := f.store.CreateEntry(ctx, f.employee.ID, core.EntryInput{Title: "Öffentlicher Basiseintrag", Body: "Nachricht aus der Werkstatt"})
	if err != nil {
		t.Fatal(err)
	}
	idea, err := f.store.CreateIdea(ctx, f.employee.ID, ideas.Input{Title: "Öffentliche Idee", Body: "Ein besserer Ablauf", Category: "Arbeitsablauf"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{entry.ID, idea.ID} {
		if err := f.store.AddComment(ctx, f.employee.ID, id, "Öffentlich lesbarer Kommentar"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.other.ID, f.other.Name, f.other.Role, false); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, f.handler)
	for _, path := range []string{"/", "/ideas", fmt.Sprintf("/entries/%d", entry.ID), fmt.Sprintf("/ideas/%d", idea.ID)} {
		w := b.request("GET", path, nil, false, "")
		if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
			t.Fatalf("public %s: %d %s", path, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), ">Abmelden</button>") {
			t.Fatalf("anonymous page %s rendered a current user", path)
		}
		if strings.HasPrefix(path, "/entries/") || strings.HasPrefix(path, "/ideas/") {
			if !strings.Contains(w.Body.String(), "Öffentlich lesbarer Kommentar") {
				t.Fatalf("public comments missing on %s", path)
			}
		}
	}
	for _, path := range []string{"/entries/new", "/ideas/new"} {
		w := b.request("GET", path, nil, false, "")
		if w.Code != http.StatusOK {
			t.Fatalf("public form %s: %d %s", path, w.Code, w.Body)
		}
		authorSelect := regexp.MustCompile(`<select\b[^>]*\bname="author_id"[^>]*>`).FindString(w.Body.String())
		if authorSelect == "" || !strings.Contains(authorSelect, "required") {
			t.Fatalf("form %s lacks required author selection", path)
		}
		if !regexp.MustCompile(`<option\b[^>]*\bvalue=""[^>]*>`).MatchString(w.Body.String()) {
			t.Fatalf("form %s lacks an empty author placeholder", path)
		}
		for _, u := range []core.User{f.employee, f.supervisor, f.admin} {
			if !strings.Contains(w.Body.String(), u.Name) {
				t.Fatalf("active author %q missing on %s", u.Name, path)
			}
		}
		if strings.Contains(w.Body.String(), f.other.Name) {
			t.Fatalf("inactive author shown on %s", path)
		}
	}
	if b.cookies[sessionCookie] != nil {
		t.Fatal("public browsing issued a user session")
	}
}

func TestAnonymousCreationUsesChosenAuthorWithoutGrantingSession(t *testing.T) {
	for _, module := range []string{"basis", "idee"} {
		t.Run(module, func(t *testing.T) {
			f := setup(t)
			b := newBrowser(t, f.handler)
			base := "/entries"
			if module == "idee" {
				base = "/ideas"
			}
			b.request("GET", base+"/new", nil, false, "")
			form := url.Values{"title": {"Ein gewählter Ersteller"}, "body": {"Signatur stammt aus der Namensliste"}, "category": {"Arbeitsablauf"}, "author_id": {strconv.FormatInt(f.admin.ID, 10)}, "user_id": {strconv.FormatInt(f.supervisor.ID, 10)}, "signature_name": {"Gefälschter Name"}, "signature_at": {"1900-01-01T00:00:00Z"}, "created_at": {"1900-01-01T00:00:00Z"}, "status": {"umgesetzt"}, "role": {"admin"}}
			auditBefore := len(publicAudit(t, f))
			if w := b.request("POST", base+"/new", form, false, ""); w.Code != http.StatusForbidden {
				t.Fatalf("anonymous create without CSRF: %d", w.Code)
			}
			start := time.Now()
			w := b.request("POST", base+"/new", form, true, "http://workshop.local")
			if w.Code != http.StatusSeeOther {
				t.Fatalf("anonymous create: %d %s", w.Code, w.Body)
			}
			e := publicCreatedEntry(t, f, module)
			if e.AuthorID != f.admin.ID || e.Signature.Name != f.admin.Name || e.Signature.At.Before(start) || e.Signature.At.After(time.Now()) || e.Status != "neu" {
				t.Fatalf("author/server signature not respected: %#v", e)
			}
			events := publicAudit(t, f)
			if len(events) != auditBefore+1 || events[0].ActorID != f.admin.ID || events[0].ActorName != f.admin.Name {
				t.Fatalf("creation audit not attributed to chosen author: %#v", events[0])
			}
			if b.cookies[sessionCookie] != nil {
				t.Fatal("choosing an admin author granted a login session")
			}
			path := fmt.Sprintf("%s/%d", base, e.ID)
			for _, target := range []struct {
				method, path string
				form         url.Values
			}{
				{"GET", path + "/edit", nil},
				{"POST", path + "/comments", url.Values{"body": {"Nicht angemeldet"}, "author_id": {strconv.FormatInt(f.admin.ID, 10)}}},
				{"POST", path + "/priority", url.Values{"side": {"vorgesetzter"}, "value": {"5"}, "version": {"1"}, "author_id": {strconv.FormatInt(f.admin.ID, 10)}}},
				{"GET", "/audit", nil},
				{"GET", "/admin/users", nil},
			} {
				result := b.request(target.method, target.path, target.form, true, "")
				if result.Code != http.StatusSeeOther || result.Header().Get("Location") != "/login" {
					t.Fatalf("anonymous protected action %s %s: %d %s", target.method, target.path, result.Code, result.Body)
				}
			}
			if len(publicAudit(t, f)) != len(events) {
				t.Fatal("blocked anonymous mutations changed audit")
			}
		})
	}
}

func TestEveryCreationRequiresAnActiveAuthor(t *testing.T) {
	for _, base := range []string{"/entries", "/ideas"} {
		t.Run(base, func(t *testing.T) {
			f := setup(t)
			if err := f.store.UpdateUser(context.Background(), f.admin.ID, f.other.ID, f.other.Name, f.other.Role, false); err != nil {
				t.Fatal(err)
			}
			b := newBrowser(t, f.handler)
			b.request("GET", base+"/new", nil, false, "")
			before := len(publicAudit(t, f))
			for _, author := range []string{"", "0", "-1", "abc", "999999", strconv.FormatInt(f.other.ID, 10)} {
				form := url.Values{"title": {"Keinen Eintrag speichern"}, "body": {"Keine gültige Auswahl"}, "category": {"Allgemein"}}
				if author != "" {
					form.Set("author_id", author)
				}
				w := b.request("POST", base+"/new", form, true, "")
				if w.Code != http.StatusBadRequest {
					t.Fatalf("author %q: %d %s", author, w.Code, w.Body)
				}
			}
			entries, err := f.store.Entries(context.Background(), f.admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			board, err := f.store.Ideas(context.Background(), f.admin.ID, ideas.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 || len(board) != 0 || len(publicAudit(t, f)) != before {
				t.Fatal("invalid author left entry or audit data")
			}
		})
	}
}

func TestAuthorChoiceDoesNotReplaceExistingSession(t *testing.T) {
	for _, base := range []string{"/entries", "/ideas"} {
		t.Run(base, func(t *testing.T) {
			f := setup(t)
			b := newBrowser(t, f.handler)
			b.login(f.employee)
			sessionBefore := b.cookies[sessionCookie].Value
			author := f.other
			if base == "/ideas" {
				author = f.admin
			}
			b.request("GET", base+"/new", nil, false, "")
			form := url.Values{"title": {"Ein anderer Ersteller"}, "body": {"Auswahl gilt nur für diesen Eintrag"}, "category": {"Allgemein"}, "author_id": {strconv.FormatInt(author.ID, 10)}}
			w := b.request("POST", base+"/new", form, true, "")
			if w.Code != http.StatusSeeOther {
				t.Fatalf("create with different author: %d %s", w.Code, w.Body)
			}
			module := "basis"
			if base == "/ideas" {
				module = "idee"
			}
			e := publicCreatedEntry(t, f, module)
			if e.AuthorID != author.ID || e.Signature.Name != author.Name || publicAudit(t, f)[0].ActorID != author.ID {
				t.Fatalf("session overrode selected author: %#v", e)
			}
			if c := b.cookies[sessionCookie]; c == nil || c.Value != sessionBefore {
				t.Fatal("author choice changed the logged-in user")
			}
			if w = b.request("GET", fmt.Sprintf("%s/%d/edit", base, e.ID), nil, false, ""); w.Code != http.StatusForbidden {
				t.Fatalf("author choice granted foreign edit access: %d %s", w.Code, w.Body)
			}
			if w = b.request("GET", "/audit", nil, false, ""); w.Code != http.StatusForbidden {
				t.Fatalf("author choice escalated role: %d", w.Code)
			}
		})
	}
}

func TestPublicBrowsingSurvivesInvalidSessionAndLogout(t *testing.T) {
	f := setup(t)
	b := newBrowser(t, f.handler)
	b.login(f.employee)
	if err := f.store.UpdateUser(context.Background(), f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	if w := b.request("GET", "/", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("inactive session blocks dashboard: %d", w.Code)
	}
	if w := b.request("GET", "/audit", nil, false, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("inactive session retains access: %d", w.Code)
	}
	h, err := New(f.store, Options{SessionTTL: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	expired := newBrowser(t, h)
	expired.login(f.supervisor)
	if w := expired.request("GET", "/", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("expired session blocks dashboard: %d", w.Code)
	}
	if w := expired.request("GET", "/audit", nil, false, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("expired session retains access: %d", w.Code)
	}
	b.login(f.supervisor)
	if w := b.request("POST", "/logout", nil, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("logout does not return to dashboard: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestEmptyInstallationCanOpenDashboard(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, h)
	if w := b.request("GET", "/", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("empty installation redirected dashboard: %d %s", w.Code, w.Body)
	}
}
