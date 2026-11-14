package storage

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tooling"
)

func toolingInput(f fixture) tooling.ToolInput {
	return tooling.ToolInput{Name: "Drehmomentschlüssel", Category: "Handwerkzeug", Location: "Werkstattwagen", Condition: "ok", AuthorID: f.admin.ID}
}
func fixtureTool(t *testing.T, f fixture) tooling.Tool {
	t.Helper()
	item, err := f.s.CreateTool(f.ctx, f.supervisor.ID, toolingInput(f))
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func fixtureToolReport(t *testing.T, f fixture, toolID int64) tooling.Report {
	t.Helper()
	item, err := f.s.CreateToolReport(f.ctx, f.employee.ID, tooling.ReportInput{ToolID: toolID, Title: "Ratsche defekt", Body: "Die Ratsche greift nicht zuverlässig.", ObservedCondition: "defekt"})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func fixturePurchase(t *testing.T, f fixture, toolID int64) tooling.Purchase {
	t.Helper()
	item, err := f.s.CreatePurchase(f.ctx, f.employee.ID, tooling.PurchaseInput{Title: "Ersatz beschaffen", Body: "Ein passendes Ersatzwerkzeug wird benötigt.", Category: "Handwerkzeug", Location: "Werkstattwagen", ToolID: toolID})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func reloadTool(t *testing.T, f fixture, id int64) tooling.Tool {
	t.Helper()
	item, err := f.s.Tool(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func reloadReport(t *testing.T, f fixture, id int64) tooling.Report {
	t.Helper()
	item, err := f.s.ToolReport(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func reloadPurchase(t *testing.T, f fixture, id int64) tooling.Purchase {
	t.Helper()
	item, err := f.s.Purchase(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestToolManagementRightsSignaturesAndVersions(t *testing.T) {
	f := newFixture(t)
	input := toolingInput(f)
	beforeAudit := len(f.audit(t))
	for _, actor := range []int64{0, 99999, f.employee.ID, f.peer.ID} {
		_, err := f.s.CreateTool(f.ctx, actor, input)
		assertError(t, err, core.ErrForbidden)
	}
	if len(f.audit(t)) != beforeAudit {
		t.Fatal("rejected tool creation changed audit")
	}
	start := time.Now()
	item, err := f.s.CreateTool(f.ctx, f.supervisor.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if item.AuthorID != f.admin.ID || item.Signature.Name != f.admin.Name || item.Signature.At.Before(start) || item.Signature.At.After(time.Now()) || item.Version != 1 || item.OpenReports != 0 {
		t.Fatalf("initial signed tool = %#v", item)
	}
	event := f.audit(t)[0]
	var created tooling.Tool
	if err := json.Unmarshal([]byte(event.After), &created); err != nil {
		t.Fatal(err)
	}
	if event.Action != "tool.create" || event.EntityType != "tool" || event.ActorID != f.supervisor.ID || created.AuthorID != f.admin.ID || created.Signature != item.Signature || event.Before != "null" {
		t.Fatalf("tool creator/audit actor confused: %#v / %#v", event, created)
	}
	update := tooling.ToolUpdate{Name: item.Name, Category: item.Category, Location: item.Location, Condition: "defekt", Version: item.Version}
	assertError(t, f.s.UpdateTool(f.ctx, f.employee.ID, item.ID, update), core.ErrForbidden)
	if err := f.s.UpdateTool(f.ctx, f.supervisor.ID, item.ID, update); err != nil {
		t.Fatal(err)
	}
	changed := reloadTool(t, f, item.ID)
	if changed.Condition != "defekt" || changed.Version != 2 || changed.Signature != item.Signature {
		t.Fatalf("tool change = %#v", changed)
	}
	event = f.audit(t)[0]
	var previous, after tooling.Tool
	if err := json.Unmarshal([]byte(event.Before), &previous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(event.After), &after); err != nil {
		t.Fatal(err)
	}
	if event.Action != "tool.update" || previous.Condition != "ok" || after.Condition != "defekt" || after.Signature != item.Signature {
		t.Fatalf("tool audit = %#v", event)
	}
	assertError(t, f.s.UpdateTool(f.ctx, f.supervisor.ID, item.ID, update), core.ErrConflict)
	update.Version = changed.Version
	count := len(f.audit(t))
	if err := f.s.UpdateTool(f.ctx, f.admin.ID, item.ID, update); err != nil {
		t.Fatal(err)
	}
	if len(f.audit(t)) != count || reloadTool(t, f, item.ID).Version != changed.Version {
		t.Fatal("tool no-op changed audit/version")
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.admin.ID, "Neuer Testadminname", core.RoleAdmin, true); err != nil {
		t.Fatal(err)
	}
	if reloadTool(t, f, item.ID).Signature != item.Signature {
		t.Fatal("user rename changed historical tool signature")
	}
	duplicate, err := f.s.CreateTool(f.ctx, f.supervisor.ID, input)
	if err != nil || duplicate.ID == item.ID {
		t.Fatalf("same-name tools must remain independent: %#v %v", duplicate, err)
	}
	for _, statement := range []string{"UPDATE tools SET signature_name='Manipuliert' WHERE id=?", "UPDATE tools SET signature_at='1970-01-01T00:00:00Z' WHERE id=?", "UPDATE tools SET author_id=99999 WHERE id=?"} {
		if _, err := f.s.db.ExecContext(f.ctx, statement, item.ID); err == nil {
			t.Fatalf("tool signature guard missing: %s", statement)
		}
	}
}

func TestToolReportIsSignedClaimAndManagersResolveIt(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	report := fixtureToolReport(t, f, tool.ID)
	if report.Module != "werkzeugmeldung" || report.Status != "neu" || report.AuthorID != f.employee.ID || report.Signature.Name != f.employee.Name || report.ToolID != tool.ID || report.ObservedCondition != "defekt" {
		t.Fatalf("report = %#v", report)
	}
	current := reloadTool(t, f, tool.ID)
	if current.Condition != "ok" || current.Version != tool.Version || current.OpenReports != 1 {
		t.Fatalf("claim overwrote official tool state or disappeared: %#v", current)
	}
	edit := tooling.ReportUpdate{Title: "Ratsche verschleißt", Body: report.Body, ObservedCondition: "verschleisst", Status: "neu", Version: report.Version}
	assertError(t, f.s.UpdateToolReport(f.ctx, f.peer.ID, report.ID, edit), core.ErrForbidden)
	edit.Status = "erledigt"
	assertError(t, f.s.UpdateToolReport(f.ctx, f.employee.ID, report.ID, edit), core.ErrForbidden)
	edit.Status = "neu"
	if err := f.s.UpdateToolReport(f.ctx, f.employee.ID, report.ID, edit); err != nil {
		t.Fatal(err)
	}
	updated := reloadReport(t, f, report.ID)
	if updated.ObservedCondition != "verschleisst" || updated.ToolID != tool.ID || updated.Signature != report.Signature || updated.Version != report.Version+1 {
		t.Fatalf("report edit = %#v", updated)
	}
	event := f.audit(t)[0]
	var previous, after tooling.Report
	if err := json.Unmarshal([]byte(event.Before), &previous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(event.After), &after); err != nil {
		t.Fatal(err)
	}
	if event.Action != "tool_report.update" || previous.ObservedCondition != "defekt" || after.ObservedCondition != "verschleisst" || after.ToolID != tool.ID {
		t.Fatalf("incomplete report audit = %#v", event)
	}
	assertError(t, f.s.UpdateToolReport(f.ctx, f.employee.ID, report.ID, edit), core.ErrConflict)
	edit.Version = updated.Version
	edit.Status = "erledigt"
	if err := f.s.UpdateToolReport(f.ctx, f.supervisor.ID, report.ID, edit); err != nil {
		t.Fatal(err)
	}
	current = reloadTool(t, f, tool.ID)
	if current.Condition != "ok" || current.OpenReports != 0 || current.Version != tool.Version {
		t.Fatalf("report resolution silently edited inventory: %#v", current)
	}
	if err := f.s.UpdateEntry(f.ctx, f.admin.ID, report.ID, core.EntryUpdate{Title: "Bypass", Body: report.Body, Status: "neu", Version: updated.Version + 1}); err == nil {
		t.Fatal("generic update accepted a tool report")
	}
	other := fixtureTool(t, f)
	if _, err := f.s.db.ExecContext(f.ctx, "UPDATE tool_reports SET tool_id=? WHERE entry_id=?", other.ID, report.ID); err == nil {
		t.Fatal("database allowed moving a report between tools")
	}
}

func TestPurchaseLifecycleRightsAndNoAutomaticInventory(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	purchase := fixturePurchase(t, f, tool.ID)
	if purchase.Module != "anschaffung" || purchase.Status != "gewuenscht" || purchase.ToolID != tool.ID || purchase.Signature.Name != f.employee.Name {
		t.Fatalf("purchase = %#v", purchase)
	}
	edit := tooling.PurchaseUpdate{Title: "Ersatz in anderer Ausführung", Body: purchase.Body, Category: purchase.Category, Location: "Werkstattregal", Status: purchase.Status, Version: purchase.Version}
	assertError(t, f.s.UpdatePurchase(f.ctx, f.peer.ID, purchase.ID, edit), core.ErrForbidden)
	edit.Status = "bestellt"
	assertError(t, f.s.UpdatePurchase(f.ctx, f.employee.ID, purchase.ID, edit), core.ErrForbidden)
	edit.Status = "gewuenscht"
	if err := f.s.UpdatePurchase(f.ctx, f.employee.ID, purchase.ID, edit); err != nil {
		t.Fatal(err)
	}
	current := reloadPurchase(t, f, purchase.ID)
	edit.Version = current.Version
	edit.Status = "genehmigt"
	if err := f.s.UpdatePurchase(f.ctx, f.supervisor.ID, purchase.ID, edit); err != nil {
		t.Fatal(err)
	}
	current = reloadPurchase(t, f, purchase.ID)
	edit.Version = current.Version
	edit.Status = current.Status
	edit.Body = "Nach Genehmigung nicht selbst ändern"
	assertError(t, f.s.UpdatePurchase(f.ctx, f.employee.ID, purchase.ID, edit), core.ErrForbidden)
	edit.Body = purchase.Body
	for _, status := range []string{"bestellt", "geliefert", "genehmigt", "geliefert"} {
		current = reloadPurchase(t, f, purchase.ID)
		edit.Version = current.Version
		edit.Status = status
		if err := f.s.UpdatePurchase(f.ctx, f.supervisor.ID, purchase.ID, edit); err != nil {
			t.Fatal(err)
		}
	}
	current = reloadPurchase(t, f, purchase.ID)
	if current.Signature != purchase.Signature || current.ToolID != tool.ID || current.Status != "geliefert" {
		t.Fatalf("purchase lifecycle damaged identity: %#v", current)
	}
	all, err := f.s.Tools(f.ctx, tooling.ToolFilter{})
	if err != nil || len(all) != 1 || all[0].ID != tool.ID {
		t.Fatalf("delivery automatically altered inventory: %#v %v", all, err)
	}
	before := len(f.audit(t))
	edit.Version = current.Version
	if err := f.s.UpdatePurchase(f.ctx, f.admin.ID, purchase.ID, edit); err != nil {
		t.Fatal(err)
	}
	if len(f.audit(t)) != before || reloadPurchase(t, f, purchase.ID).Version != current.Version {
		t.Fatal("purchase no-op created a new state")
	}
	other := fixtureTool(t, f)
	if _, err := f.s.db.ExecContext(f.ctx, "UPDATE purchases SET tool_id=? WHERE entry_id=?", other.ID, purchase.ID); err == nil {
		t.Fatal("database allowed rewriting purchase tool reference")
	}
	withoutRef, err := f.s.CreatePurchase(f.ctx, f.peer.ID, tooling.PurchaseInput{Title: "Neues Diagnosegerät", Body: "Bisher nicht vorhanden", Category: "Diagnose"})
	if err != nil || withoutRef.ToolID != 0 || withoutRef.Location != "" {
		t.Fatalf("new purchase without tool/location = %#v %v", withoutRef, err)
	}
}

func TestToolHistoryIsScopedAndPrioritiesKeepFullSnapshots(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	other := fixtureTool(t, f)
	report := fixtureToolReport(t, f, tool.ID)
	purchase := fixturePurchase(t, f, tool.ID)
	otherReport := fixtureToolReport(t, f, other.ID)
	otherPurchase := fixturePurchase(t, f, other.ID)
	for _, id := range []int64{report.ID, purchase.ID, otherReport.ID, otherPurchase.ID} {
		if err := f.s.AddComment(f.ctx, f.peer.ID, id, "Signierter Kommentar"); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		id     int64
		action string
		kind   string
	}{{report.ID, "tool_report.priority", "tool_report"}, {purchase.ID, "purchase.priority", "purchase"}} {
		value := 5
		if err := f.s.SetPriority(f.ctx, f.employee.ID, item.id, core.PriorityChange{Side: core.PriorityEmployee, Value: &value, Version: 1}); err != nil {
			t.Fatal(err)
		}
		event := f.audit(t)[0]
		if event.Action != item.action || event.EntityType != item.kind {
			t.Fatalf("wrong module priority audit: %#v", event)
		}
		if item.id == report.ID {
			var snapshot tooling.Report
			if err := json.Unmarshal([]byte(event.After), &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.ToolID != tool.ID || snapshot.ObservedCondition != report.ObservedCondition || snapshot.Signature != report.Signature {
				t.Fatalf("priority lost report context: %#v", snapshot)
			}
		} else {
			var snapshot tooling.Purchase
			if err := json.Unmarshal([]byte(event.After), &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.ToolID != tool.ID || snapshot.Category != purchase.Category || snapshot.Location != purchase.Location || snapshot.Signature != purchase.Signature {
				t.Fatalf("priority lost purchase context: %#v", snapshot)
			}
		}
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, "Umbenannter Kollege", f.peer.Role, true); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]map[int64]bool{"tool": {tool.ID: true}, "tool_report": {report.ID: true}, "purchase": {purchase.ID: true}, "comment": {}}
	for _, id := range []int64{report.ID, purchase.ID} {
		comments, err := f.s.PublicComments(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range comments {
			allowed["comment"][c.ID] = true
		}
	}
	history, err := f.s.ToolHistory(f.ctx, tool.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, event := range history {
		if !allowed[event.EntityType][event.EntityID] {
			t.Fatalf("tool history leaked unrelated audit: %#v", event)
		}
		seen[event.Action]++
	}
	for action, want := range map[string]int{"tool.create": 1, "tool_report.create": 1, "purchase.create": 1, "comment.create": 2, "tool_report.priority": 1, "purchase.priority": 1} {
		if seen[action] != want {
			t.Fatalf("history %q count = %d, want %d; events=%#v", action, seen[action], want, history)
		}
	}
	if len(history) != 7 {
		t.Fatalf("unexpected scoped history count = %d", len(history))
	}
	_, err = f.s.ToolHistory(f.ctx, 99999)
	assertError(t, err, core.ErrNotFound)
	_, err = f.s.Audit(f.ctx, 0)
	assertError(t, err, core.ErrForbidden)
}

func TestToolingValidationLiveRightsAndFilters(t *testing.T) {
	f := newFixture(t)
	input := toolingInput(f)
	before := len(f.audit(t))
	for _, bad := range []tooling.ToolInput{
		{Name: " ", Category: input.Category, Location: input.Location, Condition: "ok", AuthorID: f.admin.ID},
		{Name: input.Name, Category: " ", Location: input.Location, Condition: "ok", AuthorID: f.admin.ID},
		{Name: input.Name, Category: input.Category, Location: " ", Condition: "ok", AuthorID: f.admin.ID},
		{Name: input.Name, Category: input.Category, Location: input.Location, Condition: "unbekannt", AuthorID: f.admin.ID},
		{Name: input.Name, Category: input.Category, Location: input.Location, Condition: "ok", AuthorID: f.employee.ID},
	} {
		_, err := f.s.CreateTool(f.ctx, f.supervisor.ID, bad)
		assertError(t, err, core.ErrValidation)
	}
	tool := fixtureTool(t, f)
	for _, bad := range []tooling.ReportInput{{ToolID: 0, Title: "Titel", Body: "Beschreibung", ObservedCondition: "defekt"}, {ToolID: tool.ID, Title: "Titel", Body: "Beschreibung", ObservedCondition: "unbekannt"}, {ToolID: tool.ID, Title: "", Body: "Beschreibung", ObservedCondition: "defekt"}} {
		_, err := f.s.CreateToolReport(f.ctx, f.employee.ID, bad)
		assertError(t, err, core.ErrValidation)
	}
	for _, bad := range []tooling.PurchaseInput{{Title: "Titel", Body: "Beschreibung", Category: " "}, {Title: "Titel", Body: "Beschreibung", Category: "Diagnose", Location: strings.Repeat("ä", 101)}, {Title: "Titel", Body: "Beschreibung", Category: "Diagnose", ToolID: -1}} {
		_, err := f.s.CreatePurchase(f.ctx, f.employee.ID, bad)
		assertError(t, err, core.ErrValidation)
	}
	if len(f.audit(t)) != before+1 {
		t.Fatal("invalid input left audit events")
	}
	input.Name = "100%_Werkzeug"
	input.Condition = "verschleisst"
	literal, err := f.s.CreateTool(f.ctx, f.admin.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, search := range []string{"%", "_", "100%_"} {
		list, err := f.s.Tools(f.ctx, tooling.ToolFilter{Search: search})
		if err != nil || len(list) != 1 || list[0].ID != literal.ID {
			t.Fatalf("literal search %q = %#v %v", search, list, err)
		}
	}
	list, err := f.s.Tools(f.ctx, tooling.ToolFilter{Condition: "verschleisst", Category: input.Category})
	if err != nil || len(list) != 1 || list[0].ID != literal.ID {
		t.Fatalf("inventory filters = %#v %v", list, err)
	}
	for _, filter := range []tooling.ToolFilter{{Condition: "unbekannt"}, {Category: strings.Repeat("x", 101)}, {Search: strings.Repeat("x", 201)}} {
		_, err := f.s.Tools(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
	categories, err := f.s.ToolCategories(f.ctx)
	if err != nil || !reflect.DeepEqual(categories, []string{input.Category}) {
		t.Fatalf("tool categories = %#v %v", categories, err)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.employee.ID, f.employee.Name, f.employee.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.CreateToolReport(f.ctx, f.employee.ID, tooling.ReportInput{ToolID: tool.ID, Title: "Meldung", Body: "Beschreibung", ObservedCondition: "defekt"})
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.CreatePurchase(f.ctx, f.employee.ID, tooling.PurchaseInput{Title: "Wunsch", Body: "Beschreibung", Category: "Diagnose"})
	assertError(t, err, core.ErrForbidden)
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, f.supervisor.Name, core.RoleEmployee, true); err != nil {
		t.Fatal(err)
	}
	assertError(t, f.s.UpdateTool(f.ctx, f.supervisor.ID, tool.ID, tooling.ToolUpdate{Name: tool.Name, Category: tool.Category, Location: tool.Location, Condition: "defekt", Version: tool.Version}), core.ErrForbidden)
}

func TestToolingPurchaseAndReportPriorityFilters(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	var purchases []tooling.Purchase
	for _, pair := range [][2]*int{{priorityValue(5), priorityValue(1)}, {priorityValue(2), priorityValue(5)}, {nil, nil}} {
		item := fixturePurchase(t, f, tool.ID)
		for _, p := range []struct {
			actor int64
			side  core.PrioritySide
			value *int
		}{{f.employee.ID, core.PriorityEmployee, pair[0]}, {f.supervisor.ID, core.PrioritySupervisor, pair[1]}} {
			if p.value != nil {
				if err := f.s.SetPriority(f.ctx, p.actor, item.ID, core.PriorityChange{Side: p.side, Value: p.value, Version: item.Version}); err != nil {
					t.Fatal(err)
				}
				item.Version++
			}
		}
		purchases = append(purchases, reloadPurchase(t, f, item.ID))
	}
	for _, tc := range []struct {
		filter tooling.PurchaseFilter
		want   []int64
	}{
		{tooling.PurchaseFilter{Sort: "mitarbeiter"}, []int64{purchases[0].ID, purchases[1].ID, purchases[2].ID}},
		{tooling.PurchaseFilter{Sort: "vorgesetzter"}, []int64{purchases[1].ID, purchases[0].ID, purchases[2].ID}},
		{tooling.PurchaseFilter{ToolID: tool.ID, Category: "Handwerkzeug", EmployeePriority: priorityValue(5), SupervisorPriority: priorityValue(1)}, []int64{purchases[0].ID}},
		{tooling.PurchaseFilter{EmployeeUnset: true, SupervisorUnset: true}, []int64{purchases[2].ID}},
		{tooling.PurchaseFilter{OnlyDivergent: true}, []int64{purchases[1].ID, purchases[0].ID}},
	} {
		list, err := f.s.Purchases(f.ctx, tc.filter)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]int64, 0, len(list))
		for _, item := range list {
			ids = append(ids, item.ID)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Fatalf("purchase filter %#v = %v, want %v", tc.filter, ids, tc.want)
		}
	}
	for _, filter := range []tooling.PurchaseFilter{{Status: "erledigt"}, {Sort: "name;DROP TABLE tools"}, {EmployeePriority: priorityValue(6)}, {SupervisorPriority: priorityValue(0)}, {EmployeeUnset: true, EmployeePriority: priorityValue(1)}, {ToolID: -1}} {
		_, err := f.s.Purchases(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
	report := fixtureToolReport(t, f, tool.ID)
	other := fixtureToolReport(t, f, tool.ID)
	if err := f.s.SetPriority(f.ctx, f.employee.ID, report.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(5), Version: report.Version}); err != nil {
		t.Fatal(err)
	}
	reports, err := f.s.ToolReports(f.ctx, tooling.ReportFilter{ToolID: tool.ID, Sort: "mitarbeiter"})
	if err != nil || len(reports) != 2 || reports[0].ID != report.ID || reports[1].ID != other.ID {
		t.Fatalf("report ordering = %#v %v", reports, err)
	}
	reports, err = f.s.ToolReports(f.ctx, tooling.ReportFilter{ToolID: tool.ID, EmployeeUnset: true})
	if err != nil || len(reports) != 1 || reports[0].ID != other.ID {
		t.Fatalf("unrated reports = %#v %v", reports, err)
	}
	for _, filter := range []tooling.ReportFilter{{ToolID: -1}, {Status: "bestellt"}, {Sort: "unsafe"}, {SupervisorPriority: priorityValue(6)}, {SupervisorUnset: true, SupervisorPriority: priorityValue(1)}} {
		_, err := f.s.ToolReports(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
}

func TestToolingMutationsRollbackWithAuditFailure(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	report := fixtureToolReport(t, f, tool.ID)
	purchase := fixturePurchase(t, f, tool.ID)
	before := len(f.audit(t))
	if _, err := f.s.db.ExecContext(f.ctx, "CREATE TRIGGER tooling_test_audit_failure BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'test audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		run  func() error
	}{
		{"create tool", func() error { _, err := f.s.CreateTool(f.ctx, f.admin.ID, toolingInput(f)); return err }},
		{"update tool", func() error {
			return f.s.UpdateTool(f.ctx, f.admin.ID, tool.ID, tooling.ToolUpdate{Name: "Nicht speichern", Category: tool.Category, Location: tool.Location, Condition: "defekt", Version: tool.Version})
		}},
		{"create report", func() error {
			_, err := f.s.CreateToolReport(f.ctx, f.employee.ID, tooling.ReportInput{ToolID: tool.ID, Title: "Nicht speichern", Body: "Auditfehler", ObservedCondition: "defekt"})
			return err
		}},
		{"update report", func() error {
			return f.s.UpdateToolReport(f.ctx, f.admin.ID, report.ID, tooling.ReportUpdate{Title: "Nicht speichern", Body: report.Body, ObservedCondition: "ok", Status: "erledigt", Version: report.Version})
		}},
		{"create purchase", func() error {
			_, err := f.s.CreatePurchase(f.ctx, f.employee.ID, tooling.PurchaseInput{Title: "Nicht speichern", Body: "Auditfehler", Category: "Diagnose"})
			return err
		}},
		{"update purchase", func() error {
			return f.s.UpdatePurchase(f.ctx, f.admin.ID, purchase.ID, tooling.PurchaseUpdate{Title: "Nicht speichern", Body: purchase.Body, Category: purchase.Category, Location: purchase.Location, Status: "genehmigt", Version: purchase.Version})
		}},
		{"report priority", func() error {
			return f.s.SetPriority(f.ctx, f.employee.ID, report.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(4), Version: report.Version})
		}},
		{"purchase priority", func() error {
			return f.s.SetPriority(f.ctx, f.employee.ID, purchase.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(4), Version: purchase.Version})
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if err := mutation.run(); err == nil {
				t.Fatal("mutation survived audit failure")
			}
		})
	}
	if !reflect.DeepEqual(reloadTool(t, f, tool.ID), reloadToolValueWithOpenReport(tool)) || !reflect.DeepEqual(reloadReport(t, f, report.ID), report) || !reflect.DeepEqual(reloadPurchase(t, f, purchase.ID), purchase) {
		t.Fatal("audit failure left updated values")
	}
	tools, err := f.s.Tools(f.ctx, tooling.ToolFilter{})
	if err != nil || len(tools) != 1 {
		t.Fatal("failed tool creation left a row")
	}
	reports, err := f.s.ToolReports(f.ctx, tooling.ReportFilter{})
	if err != nil || len(reports) != 1 {
		t.Fatal("failed report creation left a row")
	}
	purchases, err := f.s.Purchases(f.ctx, tooling.PurchaseFilter{})
	if err != nil || len(purchases) != 1 || len(f.audit(t)) != before {
		t.Fatal("failed purchase creation/audit left rows")
	}
}
func reloadToolValueWithOpenReport(tool tooling.Tool) tooling.Tool { tool.OpenReports = 1; return tool }

func TestConcurrentToolReportStatusAndPriorityOnlyOneCommits(t *testing.T) {
	f := newFixture(t)
	tool := fixtureTool(t, f)
	report := fixtureToolReport(t, f, tool.ID)
	before := len(f.audit(t))
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- f.s.UpdateToolReport(f.ctx, f.supervisor.ID, report.ID, tooling.ReportUpdate{Title: report.Title, Body: report.Body, ObservedCondition: report.ObservedCondition, Status: "in_pruefung", Version: report.Version})
	}()
	go func() {
		<-start
		results <- f.s.SetPriority(f.ctx, f.employee.ID, report.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: priorityValue(5), Version: report.Version})
	}()
	close(start)
	var won, conflicted int
	for range 2 {
		err := <-results
		if err == nil {
			won++
		} else if errors.Is(err, core.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if won != 1 || conflicted != 1 || reloadReport(t, f, report.ID).Version != report.Version+1 || len(f.audit(t)) != before+1 {
		t.Fatalf("report race lost atomicity: won=%d conflict=%d", won, conflicted)
	}
}
