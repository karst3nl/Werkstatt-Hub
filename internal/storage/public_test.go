package storage

import (
	"errors"
	"reflect"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

func TestPublicBoardsExposeOnlyKnownWorkshopModules(t *testing.T) {
	f := newFixture(t)
	base := f.entry(t, f.employee.ID)
	idea := fixtureIdea(t, f, f.peer.ID, "Öffentliche Idee", "Arbeitsablauf")
	for _, id := range []int64{base.ID, idea.ID} {
		if err := f.s.AddComment(f.ctx, f.employee.ID, id, "Öffentlicher Kommentar"); err != nil {
			t.Fatal(err)
		}
	}
	// Model a future private module without weakening its production schema.
	if _, err := f.s.db.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	r, err := f.s.db.ExecContext(f.ctx, `INSERT INTO entries(module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version)
 VALUES('privat',?,'Privater Inhalt','Nicht im öffentlichen Dashboard','neu',?,?,?, ?,1)`, f.employee.ID, f.employee.Name, timestamp(base.Signature.At), timestamp(base.CreatedAt), timestamp(base.UpdatedAt))
	if err != nil {
		t.Fatal(err)
	}
	privateID, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.ExecContext(f.ctx, "INSERT INTO comments(entry_id,author_id,body,signature_name,signature_at) VALUES(?,?,?,?,?)", privateID, f.employee.ID, "Privater Kommentar", f.employee.Name, timestamp(base.Signature.At)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}

	entries, err := f.s.PublicEntries(f.ctx)
	if err != nil || len(entries) != 1 || entries[0].ID != base.ID {
		t.Fatalf("public base list = %#v, %v", entries, err)
	}
	gotBase, err := f.s.PublicEntry(f.ctx, base.ID)
	if err != nil || gotBase.Signature != base.Signature {
		t.Fatalf("public base = %#v, %v", gotBase, err)
	}
	list, err := f.s.PublicIdeas(f.ctx, ideas.Filter{})
	if err != nil || len(list) != 1 || list[0].ID != idea.ID {
		t.Fatalf("public ideas = %#v, %v", list, err)
	}
	gotIdea, err := f.s.PublicIdea(f.ctx, idea.ID)
	if err != nil || gotIdea.Category != idea.Category || gotIdea.Signature != idea.Signature {
		t.Fatalf("public idea = %#v, %v", gotIdea, err)
	}
	categories, err := f.s.PublicIdeaCategories(f.ctx)
	if err != nil || !reflect.DeepEqual(categories, []string{"Arbeitsablauf"}) {
		t.Fatalf("public categories = %#v, %v", categories, err)
	}
	for _, id := range []int64{base.ID, idea.ID} {
		comments, err := f.s.PublicComments(f.ctx, id)
		if err != nil || len(comments) != 1 || comments[0].Body != "Öffentlicher Kommentar" {
			t.Fatalf("public comments = %#v, %v", comments, err)
		}
	}
	for _, id := range []int64{idea.ID, privateID, 99999} {
		_, err := f.s.PublicEntry(f.ctx, id)
		assertError(t, err, core.ErrNotFound)
	}
	for _, id := range []int64{base.ID, privateID, 99999} {
		_, err := f.s.PublicIdea(f.ctx, id)
		assertError(t, err, core.ErrNotFound)
	}
	for _, id := range []int64{privateID, 99999} {
		_, err := f.s.PublicComments(f.ctx, id)
		assertError(t, err, core.ErrNotFound)
	}
}

func TestPublicIdeaFiltersKeepValidationAndOrdering(t *testing.T) {
	f := newFixture(t)
	first := fixtureIdea(t, f, f.employee.ID, "Priorisierte Idee", "Ordnung")
	second := fixtureIdea(t, f, f.employee.ID, "Neueste Idee", "Sicherheit")
	if err := f.s.SetPriority(f.ctx, f.employee.ID, first.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(5), Version: first.Version}); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.PublicIdeas(f.ctx, ideas.Filter{Sort: "mitarbeiter"})
	if err != nil || len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("public ordering = %#v, %v", list, err)
	}
	list, err = f.s.PublicIdeas(f.ctx, ideas.Filter{Category: "Ordnung", EmployeePriority: priorityValue(5)})
	if err != nil || len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("public filter = %#v, %v", list, err)
	}
	for _, filter := range []ideas.Filter{{Status: "unbekannt"}, {Sort: "DROP TABLE entries"}, {EmployeePriority: priorityValue(6)}} {
		_, err := f.s.PublicIdeas(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
}

func TestActiveAuthorsAndPublicHistoricalSignatures(t *testing.T) {
	f := newFixture(t)
	entry := f.entry(t, f.employee.ID)
	authors, err := f.s.ActiveAuthors(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[int64]bool)
	for _, author := range authors {
		if !author.Active {
			t.Fatal("inactive author offered")
		}
		ids[author.ID] = true
	}
	for _, user := range []core.User{f.admin, f.employee, f.peer, f.supervisor} {
		if !ids[user.ID] {
			t.Fatalf("active %s author missing", user.Role)
		}
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, "Umbenannt und inaktiv", core.RoleEmployee, false); err != nil {
		t.Fatal(err)
	}
	authors, err = f.s.ActiveAuthors(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, author := range authors {
		if author.ID == f.employee.ID {
			t.Fatal("inactive author offered after deactivation")
		}
	}
	historical, err := f.s.PublicEntry(f.ctx, entry.ID)
	if err != nil || historical.Signature != entry.Signature {
		t.Fatalf("public historical signature lost: %#v, %v", historical, err)
	}
	_, err = f.s.CreateEntry(f.ctx, f.employee.ID, core.EntryInput{Title: "Nicht speichern", Body: "Inaktiver Ersteller"})
	assertError(t, err, core.ErrForbidden)
}

func TestPublicReadsDoNotAuthorizePrivateOperations(t *testing.T) {
	f := newFixture(t)
	base := f.entry(t, f.employee.ID)
	idea := fixtureIdea(t, f, f.employee.ID, "Idee", "Ordnung")
	if _, err := f.s.PublicEntries(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ActiveAuthors(f.ctx); err != nil {
		t.Fatal(err)
	}
	before := len(f.audit(t))
	checks := []struct {
		name string
		run  func() error
	}{
		{"private entries", func() error { _, err := f.s.Entries(f.ctx, 0); return err }},
		{"private entry", func() error { _, err := f.s.Entry(f.ctx, 0, base.ID); return err }},
		{"private ideas", func() error { _, err := f.s.Ideas(f.ctx, 0, ideas.Filter{}); return err }},
		{"private idea", func() error { _, err := f.s.Idea(f.ctx, 0, idea.ID); return err }},
		{"private comments", func() error { _, err := f.s.Comments(f.ctx, 0, base.ID); return err }},
		{"audit", func() error { _, err := f.s.Audit(f.ctx, 0); return err }},
		{"create user", func() error { _, err := f.s.CreateUser(f.ctx, 0, "Unerlaubt", core.RoleAdmin); return err }},
		{"change user", func() error { return f.s.UpdateUser(f.ctx, 0, f.employee.ID, "Unerlaubt", core.RoleAdmin, true) }},
		{"comment", func() error { return f.s.AddComment(f.ctx, 0, base.ID, "Unerlaubt") }},
		{"priority", func() error {
			return f.s.SetPriority(f.ctx, 0, base.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: priorityValue(5), Version: base.Version})
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, core.ErrForbidden) {
				t.Fatalf("private operation accepted zero actor: %v", err)
			}
		})
	}
	if len(f.audit(t)) != before {
		t.Fatal("unauthorized operation changed audit log")
	}
}
