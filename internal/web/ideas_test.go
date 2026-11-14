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

func TestIdeasHTTPWorkflowAndRights(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	b := newBrowser(t, f.handler)
	if w := b.request("GET", "/ideas", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("board without session: %d", w.Code)
	}
	b.login(f.employee)
	if w := b.request("GET", "/ideas/new", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("new idea form: %d %s", w.Code, w.Body)
	}
	form := url.Values{"title": {"<script>Idee</script>"}, "body": {"Eine bessere Werkzeugablage"}, "category": {"<img src=x>"}, "author_id": {strconv.FormatInt(f.employee.ID, 10)}, "role": {"admin"}, "signature_name": {"Admin"}, "status": {"umgesetzt"}}
	if w := b.request("POST", "/ideas/new", form, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("create without csrf: %d", w.Code)
	}
	if w := b.request("POST", "/ideas/new", form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("create idea: %d %s", w.Code, w.Body)
	}
	list, err := f.store.Ideas(ctx, f.employee.ID, ideas.Filter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("saved idea: %v %#v", err, list)
	}
	i := list[0]
	if i.Module != "idee" || i.AuthorID != f.employee.ID || i.Signature.Name != f.employee.Name || i.Status != "neu" {
		t.Fatalf("forged idea fields trusted: %#v", i)
	}
	path := fmt.Sprintf("/ideas/%d", i.ID)
	w := b.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<script>Idee</script>") || strings.Contains(w.Body.String(), "<img src=x>") || !strings.Contains(w.Body.String(), "&lt;img src=x&gt;") {
		t.Fatalf("idea detail not escaped: %d %s", w.Code, w.Body)
	}
	for _, suffix := range []string{"", "/edit"} {
		w = b.request("GET", fmt.Sprintf("/entries/%d%s", i.ID, suffix), nil, false, "")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != path+suffix {
			t.Fatalf("noncanonical idea path %s: %d %q", suffix, w.Code, w.Header().Get("Location"))
		}
	}
	other := newBrowser(t, f.handler)
	other.login(f.other)
	edit := url.Values{"title": {"Bessere Ablage"}, "body": {i.Body}, "category": {"Arbeitsablauf"}, "version": {strconv.FormatInt(i.Version, 10)}}
	if w = other.request("GET", path+"/edit", nil, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("foreign edit form: %d", w.Code)
	}
	if w = other.request("POST", path+"/edit", edit, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("foreign edit: %d %s", w.Code, w.Body)
	}
	edit.Set("status", "umgesetzt")
	if w = b.request("POST", path+"/edit", edit, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("forged completed status: %d %s", w.Code, w.Body)
	}
	edit.Del("status")
	if w = b.request("POST", path+"/edit", edit, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("own category/text edit: %d %s", w.Code, w.Body)
	}
	if w = b.request("POST", path+"/edit", edit, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale idea edit: %d %s", w.Code, w.Body)
	}
	i, err = f.store.Idea(ctx, f.employee.ID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.Category != "Arbeitsablauf" || i.Status != "neu" || i.Signature.Name != f.employee.Name {
		t.Fatalf("unexpected edited idea: %#v", i)
	}
	if w = other.request("POST", path+"/comments", url.Values{"body": {"Gute Idee"}, "signature_name": {"Admin"}}, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("comment: %d %s", w.Code, w.Body)
	}
	comments, err := f.store.Comments(ctx, f.employee.ID, i.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.other.Name {
		t.Fatalf("comment signature: %v %#v", err, comments)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	edit.Set("version", strconv.FormatInt(i.Version, 10))
	edit.Set("status", "umgesetzt")
	if w = manager.request("POST", path+"/edit", edit, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("supervisor completion: %d %s", w.Code, w.Body)
	}
	w = b.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Umgesetzt") {
		t.Fatalf("completed status not displayed: %d %s", w.Code, w.Body)
	}
	// The generic editing endpoint must not change the module-specific idea.
	edit.Set("version", strconv.FormatInt(i.Version+1, 10))
	edit.Set("status", "neu")
	w = manager.request("POST", fmt.Sprintf("/entries/%d/edit", i.ID), edit, true, "")
	if w.Code == http.StatusSeeOther || w.Code == http.StatusOK {
		t.Fatalf("generic edit bypass accepted: %d %s", w.Code, w.Body)
	}
}

func TestPriorityHTTPRightsValidationAndConflict(t *testing.T) {
	for _, module := range []string{"basis", "idee"} {
		t.Run(module, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			var entry core.Entry
			var path string
			if module == "idee" {
				i, err := f.store.CreateIdea(ctx, f.employee.ID, ideas.Input{Title: "Ablage", Body: "Werkzeuge sortieren", Category: "Ordnung"})
				if err != nil {
					t.Fatal(err)
				}
				entry, path = i.Entry, fmt.Sprintf("/ideas/%d", i.ID)
			} else {
				var err error
				entry, err = f.store.CreateEntry(ctx, f.employee.ID, core.EntryInput{Title: "Ablage", Body: "Werkzeuge sortieren"})
				if err != nil {
					t.Fatal(err)
				}
				path = fmt.Sprintf("/entries/%d", entry.ID)
			}
			employee := newBrowser(t, f.handler)
			employee.login(f.other)
			form := url.Values{"side": {"mitarbeiter"}, "value": {"5"}, "version": {strconv.FormatInt(entry.Version, 10)}}
			if w := employee.request("GET", path+"/priority", nil, false, ""); w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("priority mutation via GET: %d", w.Code)
			}
			if w := employee.request("POST", path+"/priority", form, false, ""); w.Code != http.StatusForbidden {
				t.Fatalf("priority without csrf: %d", w.Code)
			}
			for _, value := range []string{"abc", "0", "6", "-1"} {
				form.Set("value", value)
				if w := employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusBadRequest {
					t.Fatalf("invalid priority %q: %d %s", value, w.Code, w.Body)
				}
			}
			form.Set("value", "5")
			form.Set("side", "admin")
			if w := employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusBadRequest {
				t.Fatalf("unknown priority side: %d", w.Code)
			}
			form.Set("side", "vorgesetzter")
			if w := employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusForbidden {
				t.Fatalf("employee overrides supervisor side: %d", w.Code)
			}
			form.Set("side", "mitarbeiter")
			if w := employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusSeeOther {
				t.Fatalf("employee cannot prioritize someone else's entry: %d %s", w.Code, w.Body)
			}
			if w := employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusConflict {
				t.Fatalf("stale priority update: %d", w.Code)
			}
			manager := newBrowser(t, f.handler)
			manager.login(f.supervisor)
			form.Set("version", strconv.FormatInt(entry.Version+1, 10))
			form.Set("value", "1")
			if w := manager.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusForbidden {
				t.Fatalf("supervisor changed foreign employee priority: %d", w.Code)
			}
			form.Set("side", "vorgesetzter")
			if w := manager.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusSeeOther {
				t.Fatalf("supervisor priority: %d %s", w.Code, w.Body)
			}
			w := employee.request("GET", path, nil, false, "")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Prioritäten weichen stark ab") || !strings.Contains(w.Body.String(), "Mitarbeiter-Priorität") || !strings.Contains(w.Body.String(), "Vorgesetzten-Priorität") {
				t.Fatalf("priorities/divergence not visible: %d %s", w.Code, w.Body)
			}
			form.Set("side", "mitarbeiter")
			form.Set("value", "")
			form.Set("version", strconv.FormatInt(entry.Version+2, 10))
			if w = employee.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusSeeOther {
				t.Fatalf("clear priority: %d %s", w.Code, w.Body)
			}
			updated, err := f.store.Entry(ctx, f.employee.ID, entry.ID)
			if err != nil || updated.EmployeePriority != nil || updated.SupervisorPriority == nil || *updated.SupervisorPriority != 1 || updated.Signature != entry.Signature {
				t.Fatalf("priority mutation damaged state: %v %#v", err, updated)
			}
		})
	}
}

func TestIdeaBoardFiltersAndOrdering(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	makeIdea := func(title, category string, employee, supervisor *int) ideas.Idea {
		t.Helper()
		i, err := f.store.CreateIdea(ctx, f.employee.ID, ideas.Input{Title: title, Body: "Beschreibung", Category: category})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []struct {
			actor int64
			side  core.PrioritySide
			value *int
		}{{f.employee.ID, core.PriorityEmployee, employee}, {f.supervisor.ID, core.PrioritySupervisor, supervisor}} {
			if p.value == nil {
				continue
			}
			if err := f.store.SetPriority(ctx, p.actor, i.ID, core.PriorityChange{Side: p.side, Value: p.value, Version: i.Version}); err != nil {
				t.Fatal(err)
			}
			i.Version++
		}
		return i
	}
	pointer := func(v int) *int { return &v }
	a := makeIdea("Idee Alpha", "Ordnung", pointer(5), pointer(1))
	b := makeIdea("Idee Beta", "Sicherheit", pointer(2), pointer(4))
	makeIdea("Idee Gamma", "Ordnung", nil, pointer(5))
	makeIdea("Idee Delta", "Ordnung", pointer(1), nil)
	if err := f.store.UpdateIdea(ctx, f.supervisor.ID, b.ID, ideas.Update{Title: b.Title, Body: b.Body, Category: b.Category, Status: "umgesetzt", Version: b.Version}); err != nil {
		t.Fatal(err)
	}
	viewer := newBrowser(t, f.handler)
	viewer.login(f.employee)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"?sort=mitarbeiter", []string{"Idee Alpha", "Idee Beta", "Idee Delta", "Idee Gamma"}},
		{"?sort=vorgesetzter", []string{"Idee Gamma", "Idee Beta", "Idee Alpha", "Idee Delta"}},
		{"?category=Ordnung&employee_priority=5&supervisor_priority=1", []string{"Idee Alpha"}},
		{"?employee_priority=unbewertet", []string{"Idee Gamma"}},
		{"?supervisor_priority=unbewertet", []string{"Idee Delta"}},
		{"?status=umgesetzt", []string{"Idee Beta"}},
		{"?divergent=1", []string{"Idee Alpha"}},
		{"?employee_priority=5&supervisor_priority=4", nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			w := viewer.request("GET", "/ideas"+tc.query, nil, false, "")
			if w.Code != http.StatusOK {
				t.Fatalf("board: %d %s", w.Code, w.Body)
			}
			body := w.Body.String()
			position := -1
			for _, title := range tc.want {
				index := strings.Index(body, ">"+title+"</a>")
				if index < 0 || index <= position {
					t.Fatalf("unexpected order/missing %q: %s", title, body)
				}
				position = index
			}
			for _, title := range []string{a.Title, b.Title, "Idee Gamma", "Idee Delta"} {
				wanted := false
				for _, present := range tc.want {
					if title == present {
						wanted = true
					}
				}
				if !wanted && strings.Contains(body, ">"+title+"</a>") {
					t.Fatalf("filtered-out idea %q present", title)
				}
			}
		})
	}
	for _, query := range []string{"?sort=unsafe", "?employee_priority=6", "?supervisor_priority=abc", "?status=erledigt"} {
		if w := viewer.request("GET", "/ideas"+query, nil, false, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid board filter %q: %d", query, w.Code)
		}
	}
}

func TestPriorityHTTPUsesConfiguredScale(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenWithOptions(filepath.Join(t.TempDir(), "scale.db"), storage.Options{PriorityScale: core.PriorityScale{Min: 2, Max: 7}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := s.BootstrapAdmin(ctx, "Admin")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateUser(ctx, a.ID, "Team", core.RoleEmployee)
	if err != nil {
		t.Fatal(err)
	}
	i, err := s.CreateIdea(ctx, e.ID, ideas.Input{Title: "Idee", Body: "Beschreibung", Category: "Allgemein"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, h)
	b.login(e)
	path := fmt.Sprintf("/ideas/%d", i.ID)
	w := b.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<option value="7"`) || strings.Contains(w.Body.String(), `<option value="1"`) {
		t.Fatalf("configured scale not rendered: %d %s", w.Code, w.Body)
	}
	form := url.Values{"side": {"mitarbeiter"}, "value": {"1"}, "version": {strconv.FormatInt(i.Version, 10)}}
	if w = b.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("below configured scale accepted: %d", w.Code)
	}
	form.Set("value", "7")
	if w = b.request("POST", path+"/priority", form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("configured upper bound rejected: %d %s", w.Code, w.Body)
	}
}
