package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
)

func taskHTTPFixture(t *testing.T, f fixture, assignee core.User, title, date string) tasks.Task {
	t.Helper()
	task, err := f.store.CreateTask(context.Background(), f.supervisor.ID, tasks.Input{Title: title, Body: "Ein klarer Arbeitsauftrag", AuthorID: f.supervisor.ID, AssigneeID: assignee.ID, DueDate: date})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func taskHTTPForm(f fixture) url.Values {
	return url.Values{"title": {"Werkzeugwagen vorbereiten"}, "body": {"Werkzeuge prüfen und übersichtlich einräumen."}, "author_id": {strconv.FormatInt(f.admin.ID, 10)}, "assignee_id": {strconv.FormatInt(f.employee.ID, 10)}, "due_date": {"2026-12-01"}}
}
func taskHTTPStored(t *testing.T, f fixture, id int64) tasks.Task {
	t.Helper()
	value, err := f.store.PublicTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func taskHTTPStatus(value tasks.Task, status string) url.Values {
	return url.Values{"status": {status}, "version": {strconv.FormatInt(value.Version, 10)}}
}

func TestTaskHTTPWorkflowAndRoleGuards(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	guest := newBrowser(t, f.handler)
	if w := guest.request("GET", "/tasks", nil, false, ""); w.Code != http.StatusOK {
		t.Fatalf("public task board: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/tasks/new", "/tasks?mine=1"} {
		if w := guest.request("GET", path, nil, false, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Fatalf("identity required for %s: %d %s", path, w.Code, w.Body)
		}
	}
	worker := newBrowser(t, f.handler)
	worker.login(f.employee)
	if w := worker.request("GET", "/tasks/new", nil, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("employee task creation form: %d", w.Code)
	}
	if w := worker.request("POST", "/tasks/new", taskHTTPForm(f), true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("employee task creation: %d %s", w.Code, w.Body)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	tokenBefore := manager.cookies[sessionCookie].Value
	if w := manager.request("GET", "/tasks/new", nil, false, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="author_id"`) || !strings.Contains(w.Body.String(), `name="assignee_id"`) || !strings.Contains(w.Body.String(), `name="due_date"`) {
		t.Fatalf("manager task form: %d %s", w.Code, w.Body)
	}
	for _, field := range []string{"author_id", "assignee_id"} {
		authorSelect := regexp.MustCompile(`<select\b[^>]*\bname="` + field + `"[^>]*>`).FindString(manager.request("GET", "/tasks/new", nil, false, "").Body.String())
		if authorSelect == "" || !strings.Contains(authorSelect, "required") {
			t.Fatalf("task form lacks required %s selection", field)
		}
	}
	if w := manager.request("POST", "/tasks/new", taskHTTPForm(f), false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("task create without csrf: %d", w.Code)
	}
	for _, tc := range []struct{ field, value string }{{"author_id", ""}, {"author_id", strconv.FormatInt(f.employee.ID, 10)}, {"assignee_id", strconv.FormatInt(f.supervisor.ID, 10)}, {"assignee_id", "999999"}, {"due_date", "2026-02-30"}} {
		form := taskHTTPForm(f)
		form.Set(tc.field, tc.value)
		if w := manager.request("POST", "/tasks/new", form, true, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s=%q: %d %s", tc.field, tc.value, w.Code, w.Body)
		}
	}
	form := taskHTTPForm(f)
	form.Set("signature_name", "Falscher Name")
	form.Set("signature_at", "1900-01-01T00:00:00Z")
	form.Set("status", "erledigt")
	w := manager.request("POST", "/tasks/new", form, true, "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("manager task creation: %d %s", w.Code, w.Body)
	}
	list, err := f.store.PublicTasks(ctx, tasks.Filter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("created tasks: %#v %v", list, err)
	}
	task := list[0]
	path := fmt.Sprintf("/tasks/%d", task.ID)
	if task.AuthorID != f.admin.ID || task.Signature.Name != f.admin.Name || task.Status != "offen" || task.Signature.At.Year() < 2020 {
		t.Fatalf("creation did not use chosen author/server signature: %#v", task)
	}
	if manager.cookies[sessionCookie].Value != tokenBefore {
		t.Fatal("task creator choice changed manager session")
	}
	events, err := f.store.Audit(ctx, f.admin.ID)
	if err != nil || events[0].Action != "task.create" || events[0].ActorID != f.supervisor.ID {
		t.Fatalf("task audit must attribute the assigning session: %#v %v", events, err)
	}
	w = guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), task.Title) || !strings.Contains(w.Body.String(), f.employee.Name) {
		t.Fatalf("public task detail: %d %s", w.Code, w.Body)
	}
	for _, suffix := range []string{"", "/edit"} {
		viewer := guest
		if suffix != "" {
			viewer = manager
		}
		r := viewer.request("GET", fmt.Sprintf("/entries/%d%s", task.ID, suffix), nil, false, "")
		if r.Code != http.StatusNotFound {
			t.Fatalf("task canonical route %q: %d %q", suffix, r.Code, r.Header().Get("Location"))
		}
	}
	peer := newBrowser(t, f.handler)
	peer.login(f.other)
	if w = peer.request("POST", path+"/status", taskHTTPStatus(task, "in_arbeit"), true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("peer changed task status: %d %s", w.Code, w.Body)
	}
	if w = worker.request("POST", path+"/status", taskHTTPStatus(task, "in_arbeit"), false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("status change without csrf: %d", w.Code)
	}
	if w = manager.request("POST", path+"/status", taskHTTPStatus(task, "erledigt"), true, ""); w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
		t.Fatalf("approval skipped review: %d %s", w.Code, w.Body)
	}
	if w = worker.request("POST", path+"/status", taskHTTPStatus(task, "in_arbeit"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("assignee starts work: %d %s", w.Code, w.Body)
	}
	if w = worker.request("POST", path+"/status", taskHTTPStatus(task, "zur_kontrolle"), true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale task status accepted: %d %s", w.Code, w.Body)
	}
	task = taskHTTPStored(t, f, task.ID)
	if w = worker.request("POST", path+"/status", taskHTTPStatus(task, "erledigt"), true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("assignee approved own task: %d %s", w.Code, w.Body)
	}
	if w = worker.request("POST", path+"/status", taskHTTPStatus(task, "zur_kontrolle"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("assignee submits for review: %d %s", w.Code, w.Body)
	}
	task = taskHTTPStored(t, f, task.ID)
	if w = worker.request("POST", path+"/edit", url.Values{"title": {"Manipuliert"}, "body": {task.Body}, "assignee_id": {strconv.FormatInt(f.other.ID, 10)}, "due_date": {task.DueDate}, "version": {strconv.FormatInt(task.Version, 10)}}, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("assignee changed assignment: %d %s", w.Code, w.Body)
	}
	if w = worker.request("POST", path+"/comments", url.Values{"body": {"Bereit für die Kontrolle"}}, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("task comment: %d %s", w.Code, w.Body)
	}
	if w = manager.request("POST", path+"/status", taskHTTPStatus(task, "erledigt"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("manager approval: %d %s", w.Code, w.Body)
	}
	approved := taskHTTPStored(t, f, task.ID)
	if approved.Status != "erledigt" || approved.Approval == nil || approved.Approval.UserID != f.supervisor.ID || approved.Approval.Signature.Name != f.supervisor.Name || approved.Signature != task.Signature {
		t.Fatalf("approval/signature: %#v", approved)
	}
	w = guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Abgenommen von") || !strings.Contains(w.Body.String(), f.supervisor.Name) {
		t.Fatalf("approval not visible: %d %s", w.Code, w.Body)
	}
	edit := url.Values{"title": {"Still ändern"}, "body": {task.Body}, "assignee_id": {strconv.FormatInt(f.employee.ID, 10)}, "due_date": {task.DueDate}, "version": {strconv.FormatInt(approved.Version, 10)}}
	if w = manager.request("POST", path+"/edit", edit, true, ""); w.Code == http.StatusOK || w.Code == http.StatusSeeOther {
		t.Fatalf("completed task edited without reopening: %d", w.Code)
	}
	edit.Set("status", "neu")
	if w = manager.request("POST", fmt.Sprintf("/entries/%d/edit", task.ID), edit, true, ""); w.Code == http.StatusOK || w.Code == http.StatusSeeOther {
		t.Fatalf("generic endpoint bypassed task rules: %d", w.Code)
	}
	if w = manager.request("POST", path+"/status", taskHTTPStatus(approved, "in_arbeit"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("manager reopening: %d %s", w.Code, w.Body)
	}
	reopened := taskHTTPStored(t, f, task.ID)
	if reopened.Approval != nil || reopened.Status != "in_arbeit" || reopened.Signature != task.Signature {
		t.Fatalf("reopening did not clear approval safely: %#v", reopened)
	}
	priority := url.Values{"side": {"mitarbeiter"}, "value": {"3"}, "version": {strconv.FormatInt(reopened.Version, 10)}}
	if w = worker.request("POST", path+"/priority", priority, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("task priority route: %d %s", w.Code, w.Body)
	}
	prioritized := taskHTTPStored(t, f, task.ID)
	if prioritized.EmployeePriority == nil || *prioritized.EmployeePriority != 3 || prioritized.Status != reopened.Status || prioritized.Signature != task.Signature {
		t.Fatalf("priority damaged task state: %#v", prioritized)
	}
	comments, err := f.store.PublicComments(ctx, task.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.employee.Name {
		t.Fatalf("task comment signature: %#v %v", comments, err)
	}

}

func TestTaskHTTPFiltersMineAndDashboardReminders(t *testing.T) {
	f := setup(t)
	settings := f.store.TaskSettings()
	now := time.Now().In(settings.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, settings.Location)
	ownOverdue := taskHTTPFixture(t, f, f.employee, "Eigene überfällige Prüfung", today.AddDate(0, 0, -1).Format("2006-01-02"))
	otherToday := taskHTTPFixture(t, f, f.other, "Fremde heutige Prüfung", today.Format("2006-01-02"))
	ownSoon := taskHTTPFixture(t, f, f.employee, "Eigene baldige Prüfung", today.AddDate(0, 0, settings.SoonDays).Format("2006-01-02"))
	done := taskHTTPFixture(t, f, f.employee, "Bereits abgenommene Prüfung", today.AddDate(0, 0, -2).Format("2006-01-02"))
	if err := f.store.ChangeTaskStatus(context.Background(), f.supervisor.ID, done.ID, tasks.StatusChange{Status: "zur_kontrolle", Version: done.Version}); err != nil {
		t.Fatal(err)
	}
	done = taskHTTPStored(t, f, done.ID)
	if err := f.store.ChangeTaskStatus(context.Background(), f.supervisor.ID, done.ID, tasks.StatusChange{Status: "erledigt", Version: done.Version}); err != nil {
		t.Fatal(err)
	}
	guest := newBrowser(t, f.handler)
	w := guest.request("GET", "/", nil, false, "")
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous dashboard: %d %s", w.Code, w.Body)
	}
	for _, text := range []string{ownOverdue.Title, otherToday.Title, ownSoon.Title, "Überfällig", "Heute fällig", "Bald fällig"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("dashboard lacks reminder %q: %s", text, w.Body)
		}
	}
	if strings.Contains(w.Body.String(), done.Title) {
		t.Fatal("completed task remained in dashboard reminders")
	}
	worker := newBrowser(t, f.handler)
	worker.login(f.employee)
	w = worker.request("GET", "/", nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ownOverdue.Title) || strings.Contains(w.Body.String(), otherToday.Title) {
		t.Fatalf("employee reminders are not personal: %d %s", w.Code, w.Body)
	}
	w = worker.request("GET", fmt.Sprintf("/tasks?mine=1&assignee_id=%d", f.other.ID), nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ownOverdue.Title) || strings.Contains(w.Body.String(), otherToday.Title) {
		t.Fatalf("mine filter could be overridden: %d %s", w.Code, w.Body)
	}
	w = guest.request("GET", fmt.Sprintf("/tasks?assignee_id=%d", f.other.ID), nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), otherToday.Title) || strings.Contains(w.Body.String(), ownOverdue.Title) {
		t.Fatalf("public assignee filter: %d %s", w.Code, w.Body)
	}
	w = guest.request("GET", "/tasks?status=erledigt", nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), done.Title) || strings.Contains(w.Body.String(), ownSoon.Title) {
		t.Fatalf("task status filter: %d %s", w.Code, w.Body)
	}
	for _, query := range []string{"?assignee_id=abc", "?assignee_id=-1", "?status=neu"} {
		if w = guest.request("GET", "/tasks"+query, nil, false, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid task filter %q: %d", query, w.Code)
		}
	}
}

func TestTaskHTTPInactiveAuthorsAndAssignees(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, f.supervisor.Role, false); err != nil {
		t.Fatal(err)
	}
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	w := admin.request("GET", "/tasks/new", nil, false, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), f.employee.Name) || strings.Contains(w.Body.String(), f.supervisor.Name) {
		t.Fatalf("inactive choices visible in task form: %d %s", w.Code, w.Body)
	}
	form := taskHTTPForm(f)
	if w := admin.request("POST", "/tasks/new", form, true, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("inactive assignee accepted: %d %s", w.Code, w.Body)
	}
	form.Set("assignee_id", strconv.FormatInt(f.other.ID, 10))
	form.Set("author_id", strconv.FormatInt(f.supervisor.ID, 10))
	if w := admin.request("POST", "/tasks/new", form, true, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("inactive author accepted: %d %s", w.Code, w.Body)
	}
	list, err := f.store.PublicTasks(ctx, tasks.Filter{})
	if err != nil || len(list) != 0 {
		t.Fatalf("invalid creation persisted task: %#v %v", list, err)
	}
}
