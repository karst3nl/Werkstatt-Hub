package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

func priorityValue(v int) *int { return &v }
func fixtureIdea(t *testing.T, f fixture, actorID int64, title, category string) ideas.Idea {
	t.Helper()
	idea, err := f.s.CreateIdea(f.ctx, actorID, ideas.Input{Title: title, Body: "Beschreibung der Idee", Category: category})
	if err != nil {
		t.Fatal(err)
	}
	return idea
}
func storedEntry(t *testing.T, f fixture, id int64) core.Entry {
	t.Helper()
	e, err := f.s.Entry(f.ctx, f.admin.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPriorityRoleRights(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    core.Role
		own     bool
		side    core.PrioritySide
		allowed bool
	}{
		{"employee own employee", core.RoleEmployee, true, core.PriorityEmployee, true},
		{"employee peer employee", core.RoleEmployee, false, core.PriorityEmployee, true},
		{"employee own supervisor", core.RoleEmployee, true, core.PrioritySupervisor, false},
		{"supervisor own employee", core.RoleSupervisor, true, core.PriorityEmployee, true},
		{"supervisor peer employee", core.RoleSupervisor, false, core.PriorityEmployee, false},
		{"supervisor peer supervisor", core.RoleSupervisor, false, core.PrioritySupervisor, true},
		{"admin own employee", core.RoleAdmin, true, core.PriorityEmployee, true},
		{"admin peer employee", core.RoleAdmin, false, core.PriorityEmployee, false},
		{"admin peer supervisor", core.RoleAdmin, false, core.PrioritySupervisor, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			actor := map[core.Role]core.User{core.RoleEmployee: f.employee, core.RoleSupervisor: f.supervisor, core.RoleAdmin: f.admin}[tc.role]
			author := f.peer.ID
			if tc.own {
				author = actor.ID
			}
			e := f.entry(t, author)
			before := len(f.audit(t))
			err := f.s.SetPriority(f.ctx, actor.ID, e.ID, core.PriorityChange{Side: tc.side, Value: priorityValue(4), Version: e.Version})
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				got := storedEntry(t, f, e.ID)
				if got.Version != e.Version+1 || len(f.audit(t)) != before+1 {
					t.Fatal("priority update not versioned and audited")
				}
			} else {
				assertError(t, err, core.ErrForbidden)
				got := storedEntry(t, f, e.ID)
				if got.Version != e.Version || got.EmployeePriority != nil || got.SupervisorPriority != nil || len(f.audit(t)) != before {
					t.Fatal("denied priority update changed stored state")
				}
			}
		})
	}
}

func TestPriorityIndependentSidesSnapshotsAndClear(t *testing.T) {
	f := newFixture(t)
	e := f.entry(t, f.employee.ID)
	initialAudit := len(f.audit(t))
	if err := f.s.SetPriority(f.ctx, f.peer.ID, e.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(1), Version: e.Version}); err != nil {
		t.Fatal(err)
	}
	first := storedEntry(t, f, e.ID)
	if first.EmployeePriority == nil || *first.EmployeePriority != 1 || first.SupervisorPriority != nil {
		t.Fatalf("first side = %#v", first)
	}
	if err := f.s.SetPriority(f.ctx, f.supervisor.ID, e.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: priorityValue(5), Version: first.Version}); err != nil {
		t.Fatal(err)
	}
	both := storedEntry(t, f, e.ID)
	if *both.EmployeePriority != 1 || both.SupervisorPriority == nil || *both.SupervisorPriority != 5 || both.Signature != e.Signature {
		t.Fatalf("independence/signature failed: %#v", both)
	}
	events := f.audit(t)
	if len(events) != initialAudit+2 {
		t.Fatal("priority changes missing audit")
	}
	latest := events[0]
	if latest.Action != "entry.priority" || latest.EntityType != "entry" || latest.ActorID != f.supervisor.ID || latest.ActorName != f.supervisor.Name || latest.At.IsZero() {
		t.Fatalf("attribution = %#v", latest)
	}
	var before, after core.Entry
	if err := json.Unmarshal([]byte(latest.Before), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(latest.After), &after); err != nil {
		t.Fatal(err)
	}
	if before.SupervisorPriority != nil || after.SupervisorPriority == nil || *after.SupervisorPriority != 5 || *after.EmployeePriority != 1 || before.Version != first.Version || after.Version != both.Version || after.Signature != e.Signature {
		t.Fatalf("priority snapshots = %#v / %#v", before, after)
	}
	if err := f.s.SetPriority(f.ctx, f.supervisor.ID, e.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: priorityValue(5), Version: both.Version}); err != nil {
		t.Fatal(err)
	}
	if len(f.audit(t)) != len(events) || storedEntry(t, f, e.ID).Version != both.Version {
		t.Fatal("no-op changed audit/version")
	}
	assertError(t, f.s.SetPriority(f.ctx, f.employee.ID, e.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(3), Version: e.Version}), core.ErrConflict)
	if err := f.s.SetPriority(f.ctx, f.employee.ID, e.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: nil, Version: both.Version}); err != nil {
		t.Fatal(err)
	}
	cleared := storedEntry(t, f, e.ID)
	if cleared.EmployeePriority != nil || cleared.SupervisorPriority == nil || *cleared.SupervisorPriority != 5 || cleared.Signature != e.Signature || cleared.Version != both.Version+1 {
		t.Fatalf("clear = %#v", cleared)
	}
	for _, in := range []core.PriorityChange{{Side: core.PriorityEmployee, Value: priorityValue(0), Version: cleared.Version}, {Side: core.PriorityEmployee, Value: priorityValue(6), Version: cleared.Version}, {Side: "fremd", Value: priorityValue(2), Version: cleared.Version}, {Side: core.PriorityEmployee, Value: priorityValue(2), Version: 0}} {
		assertError(t, f.s.SetPriority(f.ctx, f.employee.ID, e.ID, in), core.ErrValidation)
	}
	if len(f.audit(t)) != initialAudit+3 {
		t.Fatal("invalid priority change created audit event")
	}
}

func TestIdeaRightsStatusAndCategoryAudit(t *testing.T) {
	f := newFixture(t)
	idea := fixtureIdea(t, f, f.employee.ID, "Ablauf verbessern", "Arbeitsablauf")
	if idea.Module != "idee" || idea.Status != "neu" || idea.Signature.Name != f.employee.Name || idea.Signature.At.IsZero() {
		t.Fatalf("idea basis = %#v", idea)
	}
	beforeAudit := len(f.audit(t))
	in := ideas.Update{Title: "Neuer Titel", Body: idea.Body, Category: "Organisation", Status: idea.Status, Version: idea.Version}
	assertError(t, f.s.UpdateIdea(f.ctx, f.peer.ID, idea.ID, in), core.ErrForbidden)
	if len(f.audit(t)) != beforeAudit {
		t.Fatal("unauthorized idea edit audited")
	}
	if err := f.s.UpdateEntry(f.ctx, f.employee.ID, idea.ID, core.EntryUpdate{Title: "Bypass", Body: idea.Body, Status: "neu", Version: idea.Version}); err == nil {
		t.Fatal("generic entry update accepted idea")
	}
	if err := f.s.UpdateIdea(f.ctx, f.employee.ID, idea.ID, in); err != nil {
		t.Fatal(err)
	}
	edited, err := f.s.Idea(f.ctx, f.employee.ID, idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Category != "Organisation" || edited.Title != "Neuer Titel" || edited.Signature != idea.Signature {
		t.Fatalf("idea edit = %#v", edited)
	}
	event := f.audit(t)[0]
	var before, after ideas.Idea
	if err := json.Unmarshal([]byte(event.Before), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(event.After), &after); err != nil {
		t.Fatal(err)
	}
	if event.Action != "idea.update" || event.EntityType != "idea" || before.Category != "Arbeitsablauf" || after.Category != "Organisation" || after.Signature != idea.Signature {
		t.Fatalf("category audit = %#v / %#v", event, after)
	}
	in = ideas.Update{Title: edited.Title, Body: edited.Body, Category: edited.Category, Status: "umgesetzt", Version: edited.Version}
	assertError(t, f.s.UpdateIdea(f.ctx, f.employee.ID, idea.ID, in), core.ErrForbidden)
	if err := f.s.UpdateIdea(f.ctx, f.supervisor.ID, idea.ID, in); err != nil {
		t.Fatal(err)
	}
	implemented, err := f.s.Idea(f.ctx, f.peer.ID, idea.ID)
	if err != nil || implemented.Status != "umgesetzt" {
		t.Fatalf("implemented = %#v %v", implemented, err)
	}
	assertError(t, f.s.UpdateIdea(f.ctx, f.supervisor.ID, idea.ID, in), core.ErrConflict)
	in.Version = implemented.Version
	in.Status = "erledigt"
	assertError(t, f.s.UpdateIdea(f.ctx, f.admin.ID, idea.ID, in), core.ErrValidation)
	in.Status = implemented.Status
	in.Category = "  "
	assertError(t, f.s.UpdateIdea(f.ctx, f.admin.ID, idea.ID, in), core.ErrValidation)
	for _, input := range []ideas.Input{{Title: "Titel", Body: "Beschreibung", Category: " "}, {Title: "Titel", Body: "Beschreibung", Category: strings.Repeat("ä", 101)}, {Title: " ", Body: "Beschreibung", Category: "Kategorie"}} {
		_, err := f.s.CreateIdea(f.ctx, f.employee.ID, input)
		assertError(t, err, core.ErrValidation)
	}
}

func TestIdeasUseSharedCommentsAndRequireActiveActor(t *testing.T) {
	f := newFixture(t)
	base := f.entry(t, f.employee.ID)
	idea := fixtureIdea(t, f, f.employee.ID, "Idee", "Allgemein")
	entries, err := f.s.Entries(f.ctx, f.employee.ID)
	if err != nil || len(entries) != 1 || entries[0].ID != base.ID {
		t.Fatalf("base list includes ideas: %#v %v", entries, err)
	}
	for _, actor := range []core.User{f.employee, f.peer, f.supervisor, f.admin} {
		if _, err := f.s.Idea(f.ctx, actor.ID, idea.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Ideas(f.ctx, actor.ID, ideas.Filter{}); err != nil {
			t.Fatal(err)
		}
		if err := f.s.AddComment(f.ctx, actor.ID, idea.ID, "Gemeinsamer Kommentar"); err != nil {
			t.Fatal(err)
		}
	}
	comments, err := f.s.Comments(f.ctx, f.employee.ID, idea.ID)
	if err != nil || len(comments) != 4 {
		t.Fatalf("idea comments = %#v %v", comments, err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Idea(f.ctx, f.employee.ID, idea.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.Ideas(f.ctx, f.employee.ID, ideas.Filter{})
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.IdeaCategories(f.ctx, f.employee.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.CreateIdea(f.ctx, f.employee.ID, ideas.Input{Title: "Titel", Body: "Text", Category: "Kategorie"})
	assertError(t, err, core.ErrForbidden)
	assertError(t, f.s.SetPriority(f.ctx, f.employee.ID, idea.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(2), Version: idea.Version}), core.ErrForbidden)
	_, err = f.s.Idea(f.ctx, f.admin.ID, base.ID)
	assertError(t, err, core.ErrNotFound)
}

func TestIdeaAndPriorityChangesRollbackWithAuditFailure(t *testing.T) {
	f := newFixture(t)
	base := f.entry(t, f.employee.ID)
	idea := fixtureIdea(t, f, f.employee.ID, "Idee", "Ablauf")
	before := len(f.audit(t))
	if _, err := f.s.db.ExecContext(f.ctx, "CREATE TRIGGER test_m2_audit_failure BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'test audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{base.ID, idea.ID} {
		if err := f.s.SetPriority(f.ctx, f.employee.ID, id, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(4), Version: 1}); err == nil {
			t.Fatal("priority change survived missing audit")
		}
		got := storedEntry(t, f, id)
		if got.EmployeePriority != nil || got.Version != 1 {
			t.Fatal("priority rollback failed")
		}
	}
	if err := f.s.UpdateIdea(f.ctx, f.employee.ID, idea.ID, ideas.Update{Title: "Nicht speichern", Body: idea.Body, Category: "Nicht speichern", Status: idea.Status, Version: idea.Version}); err == nil {
		t.Fatal("idea update survived missing audit")
	}
	if _, err := f.s.CreateIdea(f.ctx, f.employee.ID, ideas.Input{Title: "Nicht speichern", Body: "Text", Category: "Kategorie"}); err == nil {
		t.Fatal("idea creation survived missing audit")
	}
	got, err := f.s.Idea(f.ctx, f.admin.ID, idea.ID)
	if err != nil || got.Title != idea.Title || got.Category != idea.Category || got.Version != idea.Version {
		t.Fatalf("idea rollback failed: %#v %v", got, err)
	}
	all, err := f.s.Ideas(f.ctx, f.admin.ID, ideas.Filter{})
	if err != nil || len(all) != 1 || len(f.audit(t)) != before {
		t.Fatal("failed mutations left idea or audit data")
	}
}

func TestIdeasFilterSortAndDivergence(t *testing.T) {
	f := newFixture(t)
	makeItem := func(title, category, status string, employee, supervisor *int) ideas.Idea {
		i := fixtureIdea(t, f, f.employee.ID, title, category)
		if status != "neu" {
			if err := f.s.UpdateIdea(f.ctx, f.supervisor.ID, i.ID, ideas.Update{Title: i.Title, Body: i.Body, Category: i.Category, Status: status, Version: i.Version}); err != nil {
				t.Fatal(err)
			}
		}
		if employee != nil {
			e := storedEntry(t, f, i.ID)
			if err := f.s.SetPriority(f.ctx, f.employee.ID, i.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: employee, Version: e.Version}); err != nil {
				t.Fatal(err)
			}
		}
		if supervisor != nil {
			e := storedEntry(t, f, i.ID)
			if err := f.s.SetPriority(f.ctx, f.supervisor.ID, i.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: supervisor, Version: e.Version}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := f.s.Idea(f.ctx, f.admin.ID, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	a := makeItem("A", "Ablauf", "neu", priorityValue(5), priorityValue(1))
	b := makeItem("B", "Ablauf", "geplant", priorityValue(2), priorityValue(5))
	c := makeItem("C", "Werkstatt", "neu", nil, nil)
	d := makeItem("D", "Ablauf", "neu", priorityValue(5), priorityValue(1))
	e := makeItem("E", "Werkstatt", "neu", priorityValue(1), priorityValue(3))
	for _, tc := range []struct {
		name   string
		filter ideas.Filter
		want   []int64
	}{
		{"latest", ideas.Filter{}, []int64{e.ID, d.ID, c.ID, b.ID, a.ID}},
		{"employee", ideas.Filter{Sort: "mitarbeiter"}, []int64{d.ID, a.ID, b.ID, e.ID, c.ID}},
		{"supervisor", ideas.Filter{Sort: "vorgesetzter"}, []int64{b.ID, e.ID, d.ID, a.ID, c.ID}},
		{"both and category", ideas.Filter{Category: "Ablauf", EmployeePriority: priorityValue(5), SupervisorPriority: priorityValue(1)}, []int64{d.ID, a.ID}},
		{"status", ideas.Filter{Status: "geplant"}, []int64{b.ID}},
		{"unrated employee", ideas.Filter{EmployeeUnset: true}, []int64{c.ID}},
		{"unrated supervisor", ideas.Filter{SupervisorUnset: true}, []int64{c.ID}},
		{"large divergence", ideas.Filter{OnlyDivergent: true}, []int64{d.ID, b.ID, a.ID}},
		{"nonmatching", ideas.Filter{Category: "Unbekannt"}, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.s.Ideas(f.ctx, f.employee.ID, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 0, len(got))
			for _, idea := range got {
				ids = append(ids, idea.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("IDs = %v, want %v", ids, tc.want)
			}
		})
	}
	categories, err := f.s.IdeaCategories(f.ctx, f.employee.ID)
	if err != nil || !reflect.DeepEqual(categories, []string{"Ablauf", "Werkstatt"}) {
		t.Fatalf("categories = %v %v", categories, err)
	}
	for _, filter := range []ideas.Filter{{Status: "erledigt"}, {Sort: "title;DROP TABLE entries"}, {EmployeePriority: priorityValue(0)}, {SupervisorPriority: priorityValue(6)}, {EmployeePriority: priorityValue(1), EmployeeUnset: true}, {SupervisorPriority: priorityValue(1), SupervisorUnset: true}, {Category: strings.Repeat("x", 101)}} {
		_, err := f.s.Ideas(f.ctx, f.employee.ID, filter)
		assertError(t, err, core.ErrValidation)
	}
}

func TestConfigurablePriorityScaleAndExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scale.sqlite")
	s, err := OpenWithOptions(path, Options{PriorityScale: core.PriorityScale{Min: 2, Max: 8}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, "Test Admin")
	if err != nil {
		t.Fatal(err)
	}
	idea, err := s.CreateIdea(ctx, admin.ID, ideas.Input{Title: "Test", Body: "Text", Category: "Allgemein"})
	if err != nil {
		t.Fatal(err)
	}
	assertError(t, s.SetPriority(ctx, admin.ID, idea.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(1), Version: idea.Version}), core.ErrValidation)
	if err := s.SetPriority(ctx, admin.ID, idea.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(8), Version: idea.Version}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	incompatible, err := Open(path)
	if err == nil {
		incompatible.Close()
		t.Fatal("startup accepted stored value outside configured scale")
	}
	s, err = OpenWithOptions(path, Options{PriorityScale: core.PriorityScale{Min: 2, Max: 9}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Entry(ctx, admin.ID, idea.ID)
	if err != nil || got.EmployeePriority == nil || *got.EmployeePriority != 8 {
		t.Fatalf("scale change lost stored priority: %#v %v", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, scale := range []core.PriorityScale{{Min: 0, Max: 5}, {Min: 5, Max: 5}, {Min: 5, Max: 1}, {Min: 1, Max: 101}} {
		invalid, err := OpenWithOptions(filepath.Join(t.TempDir(), "invalid.sqlite"), Options{PriorityScale: scale})
		if err == nil {
			invalid.Close()
			t.Fatalf("invalid scale accepted: %#v", scale)
		}
	}
}

func TestPriorityConcurrentUpdatesOnlyOneWins(t *testing.T) {
	f := newFixture(t)
	idea := fixtureIdea(t, f, f.employee.ID, "Gleichzeitig", "Allgemein")
	before := len(f.audit(t))
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, value := range []int{2, 4} {
		go func(value int) {
			<-start
			results <- f.s.SetPriority(f.ctx, f.employee.ID, idea.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(value), Version: idea.Version})
		}(value)
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
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 || len(f.audit(t)) != before+1 || storedEntry(t, f, idea.ID).Version != idea.Version+1 {
		t.Fatal("concurrent priority changes were silently overwritten or incorrectly audited")
	}
}
