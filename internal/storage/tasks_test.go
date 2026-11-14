package storage

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
)

func taskInput(f fixture) tasks.Input {
	return tasks.Input{Title: "Werkzeugwagen prüfen", Body: "Werkzeuge vollständig zählen und einordnen.", DueDate: "2026-11-20", AssigneeID: f.employee.ID, AuthorID: f.supervisor.ID}
}
func fixtureTask(t *testing.T, f fixture) tasks.Task {
	t.Helper()
	item, err := f.s.CreateTask(f.ctx, f.supervisor.ID, taskInput(f))
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func reloadTask(t *testing.T, f fixture, id int64) tasks.Task {
	t.Helper()
	item, err := f.s.Task(f.ctx, f.admin.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func changeTask(t *testing.T, f fixture, actorID, id int64, status string) tasks.Task {
	t.Helper()
	item := reloadTask(t, f, id)
	if err := f.s.ChangeTaskStatus(f.ctx, actorID, id, tasks.StatusChange{Status: status, Version: item.Version}); err != nil {
		t.Fatal(err)
	}
	return reloadTask(t, f, id)
}

func TestTaskCreationRequiresManagerAndUsesSelectedAuthor(t *testing.T) {
	f := newFixture(t)
	input := taskInput(f)
	input.AuthorID = f.admin.ID
	beforeAudit := len(f.audit(t))
	for _, actor := range []int64{0, f.employee.ID, f.peer.ID, 999999} {
		_, err := f.s.CreateTask(f.ctx, actor, input)
		assertError(t, err, core.ErrForbidden)
	}
	if len(f.audit(t)) != beforeAudit {
		t.Fatal("denied task creation changed audit")
	}
	before := time.Now()
	item, err := f.s.CreateTask(f.ctx, f.supervisor.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if item.Module != "aufgabe" || item.Status != "offen" || item.Version != 1 || item.AssigneeID != f.employee.ID || item.AssigneeName != f.employee.Name || item.DueDate != input.DueDate || item.Approval != nil {
		t.Fatalf("initial task = %#v", item)
	}
	if item.AuthorID != f.admin.ID || item.Signature.Name != f.admin.Name || item.Signature.At.Before(before) || item.Signature.At.After(time.Now()) {
		t.Fatalf("selected author not signed by server: %#v", item)
	}
	event := f.audit(t)[0]
	var snapshot tasks.Task
	if err := json.Unmarshal([]byte(event.After), &snapshot); err != nil {
		t.Fatal(err)
	}
	if event.Action != "task.create" || event.EntityType != "task" || event.ActorID != f.supervisor.ID || event.ActorName != f.supervisor.Name || snapshot.AuthorID != f.admin.ID || snapshot.AssigneeID != f.employee.ID || snapshot.Signature != item.Signature || event.Before != "null" {
		t.Fatalf("task creator and acting manager confused: %#v / %#v", event, snapshot)
	}
}

func TestTaskAssigneeWorkflowApprovalReopenAndReassignment(t *testing.T) {
	f := newFixture(t)
	item := fixtureTask(t, f)
	signature := item.Signature
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.peer.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version}), core.ErrForbidden)
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.supervisor.ID, item.ID, tasks.StatusChange{Status: "erledigt", Version: item.Version}), core.ErrForbidden)
	edit := tasks.Update{Title: "Nicht erlaubt", Body: item.Body, DueDate: item.DueDate, AssigneeID: item.AssigneeID, Version: item.Version}
	assertError(t, f.s.UpdateTask(f.ctx, f.employee.ID, item.ID, edit), core.ErrForbidden)
	before := len(f.audit(t))
	if err := f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: item.Status, Version: item.Version}); err != nil {
		t.Fatal(err)
	}
	if len(f.audit(t)) != before || reloadTask(t, f, item.ID).Version != item.Version {
		t.Fatal("unchanged status created an audit event or version")
	}
	item = changeTask(t, f, f.employee.ID, item.ID, "in_arbeit")
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "zur_kontrolle", Version: 1}), core.ErrConflict)
	item = changeTask(t, f, f.employee.ID, item.ID, "zur_kontrolle")
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "erledigt", Version: item.Version}), core.ErrForbidden)
	approvedAt := time.Now()
	item = changeTask(t, f, f.supervisor.ID, item.ID, "erledigt")
	if item.Approval == nil || item.Approval.UserID != f.supervisor.ID || item.Approval.Signature.Name != f.supervisor.Name || item.Approval.Signature.At.Before(approvedAt) || item.Approval.Signature.At.After(time.Now()) || item.Signature != signature {
		t.Fatalf("approval attribution = %#v", item)
	}
	approval := *item.Approval
	if _, err := f.s.db.ExecContext(f.ctx, "UPDATE tasks SET approved_name='Manipuliert' WHERE entry_id=?", item.ID); err == nil {
		t.Fatal("database allowed overwriting an approval signature")
	}
	event := f.audit(t)[0]
	var previous, approved tasks.Task
	if err := json.Unmarshal([]byte(event.Before), &previous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(event.After), &approved); err != nil {
		t.Fatal(err)
	}
	if event.Action != "task.approve" || event.ActorID != f.supervisor.ID || previous.Status != "zur_kontrolle" || previous.Approval != nil || approved.Approval == nil || approved.Approval.Signature != approval.Signature {
		t.Fatalf("approval not audited with full task: %#v", event)
	}
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version}), core.ErrForbidden)
	edit = tasks.Update{Title: "Geänderter Auftrag", Body: item.Body, DueDate: item.DueDate, AssigneeID: item.AssigneeID, Version: item.Version}
	if err := f.s.UpdateTask(f.ctx, f.admin.ID, item.ID, edit); err == nil {
		t.Fatal("completed task content changed without reopening")
	}
	value := 4
	if err := f.s.SetPriority(f.ctx, f.supervisor.ID, item.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: &value, Version: item.Version}); err != nil {
		t.Fatal(err)
	}
	item = reloadTask(t, f, item.ID)
	if item.Approval == nil || *item.Approval != approval || item.Signature != signature {
		t.Fatal("priority change invalidated approval or creation signature")
	}
	priorityEvent := f.audit(t)[0]
	var prioritized tasks.Task
	if err := json.Unmarshal([]byte(priorityEvent.After), &prioritized); err != nil {
		t.Fatal(err)
	}
	if priorityEvent.Action != "task.priority" || priorityEvent.EntityType != "task" || prioritized.AssigneeID != item.AssigneeID || prioritized.DueDate != item.DueDate || prioritized.Approval == nil {
		t.Fatalf("task priority lost module snapshot: %#v", priorityEvent)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, "Neue Testleitung", core.RoleSupervisor, true); err != nil {
		t.Fatal(err)
	}
	if got := reloadTask(t, f, item.ID); got.Approval.Signature != approval.Signature || got.Signature != signature {
		t.Fatal("user rename rewrote historical task signatures")
	}
	item = changeTask(t, f, f.supervisor.ID, item.ID, "in_arbeit")
	if item.Approval != nil || item.Signature != signature {
		t.Fatal("reopen did not clear approval while preserving author signature")
	}
	edit = tasks.Update{Title: "Neuer Auftrag", Body: item.Body, DueDate: "2026-11-22", AssigneeID: f.peer.ID, Version: item.Version}
	if err := f.s.UpdateTask(f.ctx, f.supervisor.ID, item.ID, edit); err != nil {
		t.Fatal(err)
	}
	item = reloadTask(t, f, item.ID)
	if item.AssigneeID != f.peer.ID || item.AssigneeName != f.peer.Name || item.Status != "offen" || item.DueDate != edit.DueDate || item.Signature != signature {
		t.Fatalf("reassignment = %#v", item)
	}
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version}), core.ErrForbidden)
	changeTask(t, f, f.peer.ID, item.ID, "in_arbeit")
}

func TestTaskInputValidationAndLiveUserRights(t *testing.T) {
	f := newFixture(t)
	before := len(f.audit(t))
	invalid := []tasks.Input{}
	for _, date := range []string{"", "2026-02-29", "2024-2-29", "2026-11-20T10:00:00Z", "0000-01-01"} {
		in := taskInput(f)
		in.DueDate = date
		invalid = append(invalid, in)
	}
	for _, assignee := range []int64{0, 999999, f.supervisor.ID, f.admin.ID} {
		in := taskInput(f)
		in.AssigneeID = assignee
		invalid = append(invalid, in)
	}
	for _, author := range []int64{0, 999999, f.employee.ID} {
		in := taskInput(f)
		in.AuthorID = author
		invalid = append(invalid, in)
	}
	for _, title := range []string{" ", strings.Repeat("ä", 201)} {
		in := taskInput(f)
		in.Title = title
		invalid = append(invalid, in)
	}
	for _, in := range invalid {
		_, err := f.s.CreateTask(f.ctx, f.supervisor.ID, in)
		assertError(t, err, core.ErrValidation)
	}
	if len(f.audit(t)) != before {
		t.Fatal("invalid task left audit events")
	}
	in := taskInput(f)
	in.DueDate = "2028-02-29"
	item, err := f.s.CreateTask(f.ctx, f.supervisor.ID, in)
	if err != nil {
		t.Fatalf("valid leap date rejected: %v", err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.CreateTask(f.ctx, f.supervisor.ID, in)
	assertError(t, err, core.ErrValidation)
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version}), core.ErrForbidden)
	_, err = f.s.Task(f.ctx, f.employee.ID, item.ID)
	assertError(t, err, core.ErrForbidden)
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, core.RoleSupervisor, false); err != nil {
		t.Fatal(err)
	}
	in.AuthorID, in.AssigneeID = f.supervisor.ID, f.peer.ID
	_, err = f.s.CreateTask(f.ctx, f.admin.ID, in)
	assertError(t, err, core.ErrValidation)
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, core.RoleEmployee, true); err != nil {
		t.Fatal(err)
	}
	in.AuthorID = f.admin.ID
	in.AssigneeID = f.peer.ID
	_, err = f.s.CreateTask(f.ctx, f.supervisor.ID, in)
	assertError(t, err, core.ErrForbidden)
	assertError(t, f.s.ChangeTaskStatus(f.ctx, f.supervisor.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version}), core.ErrForbidden)
}

func TestTaskPublicReadFiltersAndSharedComments(t *testing.T) {
	f := newFixture(t)
	a := fixtureTask(t, f)
	input := taskInput(f)
	input.DueDate = "2026-11-19"
	input.AssigneeID = f.peer.ID
	b, err := f.s.CreateTask(f.ctx, f.admin.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []core.User{f.employee, f.peer, f.supervisor, f.admin} {
		if _, err := f.s.Task(f.ctx, actor.ID, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.Task(f.ctx, 0, a.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("anonymous private task API: %v", err)
	}
	if _, err := f.s.Tasks(f.ctx, 0, tasks.Filter{}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("anonymous private task list API: %v", err)
	}
	list, err := f.s.PublicTasks(f.ctx, tasks.Filter{})
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("public task ordering: %#v %v", list, err)
	}
	list, err = f.s.PublicTasks(f.ctx, tasks.Filter{AssigneeID: f.employee.ID, Status: "offen"})
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("task filter: %#v %v", list, err)
	}
	for _, filter := range []tasks.Filter{{AssigneeID: -1}, {Status: "umgesetzt"}} {
		_, err := f.s.PublicTasks(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
	if _, err := f.s.PublicTask(f.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	base := f.entry(t, f.employee.ID)
	_, err = f.s.PublicTask(f.ctx, base.ID)
	assertError(t, err, core.ErrNotFound)
	if err := f.s.AddComment(f.ctx, f.peer.ID, a.ID, "Rückfrage zur Aufgabe"); err != nil {
		t.Fatal(err)
	}
	comments, err := f.s.PublicComments(f.ctx, a.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.peer.Name {
		t.Fatalf("shared task comments: %#v %v", comments, err)
	}
	assignees, err := f.s.TaskAssignees(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[int64]bool)
	for _, u := range assignees {
		ids[u.ID] = true
		if !u.Active || u.Role != core.RoleEmployee {
			t.Fatalf("invalid assignee offered: %#v", u)
		}
	}
	if !ids[f.employee.ID] || !ids[f.peer.ID] || ids[f.admin.ID] || ids[f.supervisor.ID] {
		t.Fatalf("assignee list = %#v", assignees)
	}
}

func TestTaskMutationsRollbackOnAuditFailure(t *testing.T) {
	for _, mutation := range []string{"create", "update", "status", "approve", "priority"} {
		t.Run(mutation, func(t *testing.T) {
			f := newFixture(t)
			item := fixtureTask(t, f)
			if mutation == "approve" {
				item = changeTask(t, f, f.employee.ID, item.ID, "zur_kontrolle")
			}
			before := len(f.audit(t))
			original := reloadTask(t, f, item.ID)
			if _, err := f.s.db.ExecContext(f.ctx, "CREATE TRIGGER test_task_audit_failure BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'test audit unavailable'); END"); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mutation {
			case "create":
				_, err = f.s.CreateTask(f.ctx, f.supervisor.ID, taskInput(f))
			case "update":
				err = f.s.UpdateTask(f.ctx, f.supervisor.ID, item.ID, tasks.Update{Title: "Nicht speichern", Body: item.Body, DueDate: "2026-11-21", AssigneeID: f.peer.ID, Version: item.Version})
			case "status":
				err = f.s.ChangeTaskStatus(f.ctx, f.employee.ID, item.ID, tasks.StatusChange{Status: "in_arbeit", Version: item.Version})
			case "approve":
				err = f.s.ChangeTaskStatus(f.ctx, f.supervisor.ID, item.ID, tasks.StatusChange{Status: "erledigt", Version: item.Version})
			case "priority":
				v := 4
				err = f.s.SetPriority(f.ctx, f.supervisor.ID, item.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: &v, Version: item.Version})
			}
			if err == nil {
				t.Fatalf("%s survived missing audit", mutation)
			}
			current := reloadTask(t, f, item.ID)
			list, readErr := f.s.PublicTasks(f.ctx, tasks.Filter{})
			if readErr != nil || len(list) != 1 || !reflect.DeepEqual(current, original) || len(f.audit(t)) != before {
				t.Fatalf("%s left data after audit failure: %#v %v", mutation, current, readErr)
			}
		})
	}
}

func TestConcurrentTaskApprovalAndReassignmentCannotBothCommit(t *testing.T) {
	f := newFixture(t)
	item := fixtureTask(t, f)
	item = changeTask(t, f, f.employee.ID, item.ID, "zur_kontrolle")
	before := len(f.audit(t))
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- f.s.ChangeTaskStatus(f.ctx, f.supervisor.ID, item.ID, tasks.StatusChange{Status: "erledigt", Version: item.Version})
	}()
	go func() {
		<-start
		results <- f.s.UpdateTask(f.ctx, f.admin.ID, item.ID, tasks.Update{Title: item.Title, Body: item.Body, DueDate: item.DueDate, AssigneeID: f.peer.ID, Version: item.Version})
	}()
	close(start)
	success := 0
	failed := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, core.ErrConflict) || errors.Is(err, core.ErrValidation) || errors.Is(err, core.ErrForbidden) {
			failed++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || failed != 1 || len(f.audit(t)) != before+1 {
		t.Fatal("approval and reassignment silently overwrote one another")
	}
	current := reloadTask(t, f, item.ID)
	if current.Version != item.Version+1 {
		t.Fatalf("unexpected shared version %d", current.Version)
	}
	if current.Status == "erledigt" && (current.AssigneeID != item.AssigneeID || current.Approval == nil) {
		t.Fatal("approval applied to reassigned task")
	}
	if current.AssigneeID == f.peer.ID && (current.Status != "offen" || current.Approval != nil) {
		t.Fatal("reassignment retained completed workflow")
	}
}
