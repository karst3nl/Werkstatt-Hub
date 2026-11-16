package storage

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

func materialFixture(t *testing.T, f fixture, stock, minimum int64) materials.Material {
	t.Helper()
	m, err := f.s.CreateMaterial(f.ctx, f.admin.ID, materials.Input{Name: "Scheibenklar", Unit: "Liter", Location: "Lager", Stock: stock, Minimum: minimum})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func materialUpdate(m materials.Material) materials.Update {
	return materials.Update{Name: m.Name, Location: m.Location, Minimum: m.Minimum, Active: m.Active, Version: m.Version}
}
func countMaterial(t *testing.T, f fixture, m materials.Material, authorID, quantity int64) materials.Material {
	t.Helper()
	c, err := f.s.RecordMaterialCheck(f.ctx, authorID, m.ID, materials.CheckInput{Quantity: quantity, Version: m.Version})
	if err != nil {
		t.Fatal(err)
	}
	if c.Quantity != quantity || c.MaterialID != m.ID || c.Module != materials.ModuleCheck || c.Status != materials.CheckStatus {
		t.Fatalf("wrong check: %#v", c)
	}
	updated, err := f.s.Material(f.ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}
func materialDashboardCount(t *testing.T, f fixture, want int) {
	t.Helper()
	d, err := f.s.MaterialDashboard(f.ctx)
	if err != nil || d.OpenCount != want {
		t.Fatalf("dashboard=%#v,%v, want count%d", d, err, want)
	}
}

func TestMaterialsRightsAndOptimisticVersions(t *testing.T) {
	f := newFixture(t)
	for _, id := range []int64{0, f.employee.ID, f.supervisor.ID} {
		_, err := f.s.CreateMaterial(f.ctx, id, materials.Input{Name: "Nicht erlaubt", Unit: "Stück", Stock: 5000, Minimum: 1000})
		assertError(t, err, core.ErrForbidden)
	}
	m := materialFixture(t, f, 5000, 1000)
	for _, id := range []int64{0, f.employee.ID, f.supervisor.ID} {
		_, err := f.s.UpdateMaterial(f.ctx, id, m.ID, materialUpdate(m))
		assertError(t, err, core.ErrForbidden)
	}
	for _, u := range []core.User{f.employee, f.supervisor, f.admin} {
		old := m
		m = countMaterial(t, f, m, u.ID, m.Stock)
		if m.Version != old.Version+1 {
			t.Fatal("a count must increment materialversion even if quantity matches")
		}
		_, err := f.s.RecordMaterialCheck(f.ctx, u.ID, m.ID, materials.CheckInput{Quantity: 1000, Version: old.Version})
		assertError(t, err, core.ErrConflict)
		_, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, materialUpdate(old))
		assertError(t, err, core.ErrConflict)
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, f.peer.Role, false); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.RecordMaterialCheck(f.ctx, f.peer.ID, m.ID, materials.CheckInput{Quantity: 0, Version: m.Version})
	assertError(t, err, core.ErrForbidden)
	if err = f.s.DeleteUser(f.ctx, f.admin.ID, f.peer.ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.RecordMaterialCheck(f.ctx, f.peer.ID, m.ID, materials.CheckInput{Quantity: 0, Version: m.Version})
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.RecordMaterialCheck(f.ctx, 0, m.ID, materials.CheckInput{Quantity: 0, Version: m.Version})
	assertError(t, err, core.ErrForbidden)
}

func TestMaterialWarningCycleRequiresExplicitAcknowledgment(t *testing.T) {
	f := newFixture(t)
	m := materialFixture(t, f, 5000, 1000)
	materialDashboardCount(t, f, 0)
	m = countMaterial(t, f, m, f.employee.ID, 1000)
	first := m.OpenAlertID
	if first == 0 {
		t.Fatal("threshold equality did not warn")
	}
	materialDashboardCount(t, f, 1)
	m = countMaterial(t, f, m, f.peer.ID, 1000)
	if m.OpenAlertID != first {
		t.Fatal("same count duplicated openwarning")
	}
	m = countMaterial(t, f, m, f.employee.ID, 5000)
	if m.OpenAlertID != first {
		t.Fatal("replenishment automatically acknowledged warning")
	}
	_, err := f.s.AcknowledgeMaterialAlert(f.ctx, f.employee.ID, m.ID, first)
	assertError(t, err, core.ErrForbidden)
	ack, err := f.s.AcknowledgeMaterialAlert(f.ctx, f.supervisor.ID, m.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Acknowledgment == nil || ack.Acknowledgment.Signature.Name != f.supervisor.Name {
		t.Fatal("missing signedacknowledgment")
	}
	materialDashboardCount(t, f, 0)
	m, _ = f.s.Material(f.ctx, m.ID)
	m = countMaterial(t, f, m, f.employee.ID, 1000)
	second := m.OpenAlertID
	if second == 0 || second == first {
		t.Fatal("new stockdrop did not create new warning")
	}
	_, err = f.s.AcknowledgeMaterialAlert(f.ctx, f.admin.ID, m.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = f.s.Material(f.ctx, m.ID)
	m = countMaterial(t, f, m, f.employee.ID, 1000)
	if m.OpenAlertID != 0 {
		t.Fatal("same low count reopened acknowledgedwarning")
	}
	m = countMaterial(t, f, m, f.employee.ID, 1000)
	materialDashboardCount(t, f, 0)
	m = countMaterial(t, f, m, f.employee.ID, 500)
	if m.OpenAlertID == 0 {
		t.Fatal("furtherdrop belowminimum did not reopen")
	}
	third := m.OpenAlertID
	_, err = f.s.AcknowledgeMaterialAlert(f.ctx, f.admin.ID, m.ID, second)
	assertError(t, err, core.ErrConflict)
	_, err = f.s.AcknowledgeMaterialAlert(f.ctx, f.admin.ID, m.ID+999, third)
	assertError(t, err, core.ErrNotFound)
	all, err := f.s.MaterialAlerts(f.ctx, m.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("warninghistory=%#v,%v", all, err)
	}
}

func TestMaterialThresholdCrossingWhileInactiveIsPreserved(t *testing.T) {
	f := newFixture(t)
	m := materialFixture(t, f, 5000, 1000)
	in := materialUpdate(m)
	in.Active = false
	m, _ = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
	in = materialUpdate(m)
	in.Minimum = 6000
	var err error
	m, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.OpenAlertID == 0 {
		t.Fatal("thresholdcross whileinactive discarded")
	}
	opening := m.OpenAlertID
	materialDashboardCount(t, f, 0)
	_, err = f.s.RecordMaterialCheck(f.ctx, f.employee.ID, m.ID, materials.CheckInput{Quantity: 1000, Version: m.Version})
	assertError(t, err, core.ErrForbidden)
	in = materialUpdate(m)
	in.Active = true
	m, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.OpenAlertID != opening {
		t.Fatal("reactivation replaced existingwarning")
	}
	materialDashboardCount(t, f, 1)
	_, err = f.s.AcknowledgeMaterialAlert(f.ctx, f.supervisor.ID, m.ID, opening)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = f.s.Material(f.ctx, m.ID)
	in = materialUpdate(m)
	in.Minimum = 7000
	m, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.OpenAlertID != 0 {
		t.Fatal("alreadyacknowledged lowstock reopened withoutcrossing")
	}
}

func TestMaterialInitialLowAndInputValidation(t *testing.T) {
	f := newFixture(t)
	m := materialFixture(t, f, 0, 0)
	if m.OpenAlertID == 0 {
		t.Fatal("initial0atminimum0 mustwarn")
	}
	for _, in := range []materials.Input{
		{Name: "", Unit: "Liter"}, {Name: "Material", Unit: ""}, {Name: "Material", Unit: "Liter", Stock: -1}, {Name: "Material", Unit: "Liter", Minimum: -1},
		{Name: "Material", Unit: "Liter", Stock: materials.MaxQuantity + 1}, {Name: "Material", Unit: "Liter", Minimum: materials.MaxQuantity + 1},
		{Name: strings.Repeat("a", 201), Unit: "Liter"}, {Name: "Material", Unit: strings.Repeat("a", 51)}, {Name: "Material", Unit: "Liter", Location: strings.Repeat("a", 201)},
		{Name: "Material\x00", Unit: "Liter"}, {Name: "Material", Unit: "Liter", Location: string([]byte{0xff})},
	} {
		_, err := f.s.CreateMaterial(f.ctx, f.admin.ID, in)
		assertError(t, err, core.ErrValidation)
	}
	for _, in := range []materials.CheckInput{{Quantity: -1, Version: m.Version}, {Quantity: materials.MaxQuantity + 1, Version: m.Version}, {Quantity: 1000}, {Quantity: 1000, Version: m.Version, Note: "\x00"}, {Quantity: 1000, Version: m.Version, Note: strings.Repeat("ä", 20001)}} {
		_, err := f.s.RecordMaterialCheck(f.ctx, f.employee.ID, m.ID, in)
		assertError(t, err, core.ErrValidation)
	}
	max := materialFixture(t, f, materials.MaxQuantity, materials.MaxQuantity)
	max = countMaterial(t, f, max, f.employee.ID, materials.MaxQuantity)
	if max.Stock != materials.MaxQuantity {
		t.Fatal("fixedquantitylostprecision")
	}
}

func TestMaterialHistorySignaturesCommentsPrioritiesAndGuards(t *testing.T) {
	f := newFixture(t)
	m := materialFixture(t, f, 5000, 1000)
	c, err := f.s.RecordMaterialCheck(f.ctx, f.employee.ID, m.ID, materials.CheckInput{Quantity: 1000, Note: "Gezählt und geprüft", Version: m.Version})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = f.s.Material(f.ctx, m.ID)
	if err = f.s.AddComment(f.ctx, f.peer.ID, c.ID, "Nächste Lieferung prüfen"); err != nil {
		t.Fatal(err)
	}
	comments, err := f.s.PublicComments(f.ctx, c.ID)
	if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.peer.Name {
		t.Fatalf("comments=%#v,%v", comments, err)
	}
	value := 5
	err = f.s.SetPriority(f.ctx, f.peer.ID, c.ID, core.PriorityChange{Side: core.PriorityEmployee, Value: &value, Version: c.Version})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = f.s.MaterialCheck(f.ctx, c.ID)
	err = f.s.SetPriority(f.ctx, f.employee.ID, c.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: &value, Version: c.Version})
	assertError(t, err, core.ErrForbidden)
	err = f.s.SetPriority(f.ctx, f.supervisor.ID, c.ID, core.PriorityChange{Side: core.PrioritySupervisor, Value: &value, Version: c.Version})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = f.s.MaterialCheck(f.ctx, c.ID)
	if c.EmployeePriority == nil || *c.EmployeePriority != 5 || c.SupervisorPriority == nil || *c.SupervisorPriority != 5 {
		t.Fatal("independentprioritiesnotstored")
	}
	ack, err := f.s.AcknowledgeMaterialAlert(f.ctx, f.supervisor.ID, m.ID, m.OpenAlertID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, "Umbenannte Leitung", f.supervisor.Role, true); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DeleteUser(f.ctx, f.admin.ID, f.employee.ID); err != nil {
		t.Fatal(err)
	}
	preserved, _ := f.s.MaterialCheck(f.ctx, c.ID)
	alerts, _ := f.s.MaterialAlerts(f.ctx, m.ID)
	if preserved.Signature.Name != f.employee.Name || preserved.Body != "Gezählt und geprüft" || alerts[0].Signature.Name != f.employee.Name || alerts[0].Acknowledgment.Signature != ack.Acknowledgment.Signature {
		t.Fatal("historicalsignaturemodified")
	}
	for _, query := range []string{
		fmt.Sprintf("UPDATE materials SET unit='Kiste' WHERE id=%d", m.ID), fmt.Sprintf("DELETE FROM materials WHERE id=%d", m.ID),
		fmt.Sprintf("UPDATE entries SET body='Manipuliert' WHERE id=%d", c.ID), fmt.Sprintf("UPDATE entries SET status='neu' WHERE id=%d", c.ID),
		fmt.Sprintf("UPDATE material_checks SET quantity=999 WHERE entry_id=%d", c.ID), fmt.Sprintf("DELETE FROM material_checks WHERE entry_id=%d", c.ID),
		fmt.Sprintf("UPDATE material_alerts SET quantity=999 WHERE id=%d", m.OpenAlertID), fmt.Sprintf("UPDATE material_alerts SET acknowledged_name='Manipuliert' WHERE id=%d", m.OpenAlertID), fmt.Sprintf("DELETE FROM material_alerts WHERE id=%d", m.OpenAlertID),
	} {
		if _, err = f.s.db.Exec(query); err == nil {
			t.Fatalf("immutableguardmissing: %s", query)
		}
	}
	events := f.audit(t)
	found := false
	for _, e := range events {
		if e.Action == "material_check.priority" && e.EntityType == "material_check" {
			found = true
		}
	}
	if !found {
		t.Fatal("materialpriorityauditmissing")
	}
}

func TestMaterialConcurrentAcknowledgmentExactlyOnce(t *testing.T) {
	f := newFixture(t)
	m := materialFixture(t, f, 1000, 1000)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int64{f.admin.ID, f.supervisor.ID} {
		wg.Add(1)
		go func(actorID int64) {
			defer wg.Done()
			_, err := f.s.AcknowledgeMaterialAlert(f.ctx, actorID, m.ID, m.OpenAlertID)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	var success, conflict int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, core.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	count := 0
	for _, e := range f.audit(t) {
		if e.Action == "material.alert_acknowledge" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ackaudits=%d", count)
	}
}

func materialSnapshot(t *testing.T, f fixture) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{"materials", "material_checks", "material_alerts", "entries", "audit_log"} {
		var count int
		if err := f.s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		result[table] = fmt.Sprint(count)
	}
	rows, err := f.s.Materials(f.ctx, materials.Filter{Activity: "alle"})
	if err != nil {
		t.Fatal(err)
	}
	result["state"] = fmt.Sprintf("%#v", rows)
	// Serialize values instead of pointer addresses for reliable before/after comparison.
	for _, m := range rows {
		result[fmt.Sprint(m.ID)] = fmt.Sprintf("%d/%d/%d/%d/%t/%s", m.Stock, m.Minimum, m.Version, m.OpenAlertID, m.Active, m.UpdatedAt)
	}
	delete(result, "state")
	return result
}
func TestMaterialWritesRollbackWithAuditFailure(t *testing.T) {
	for _, action := range []string{"material.create", "material.update", "material_check.create", "material.alert_open", "material.alert_acknowledge"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			m := materialFixture(t, f, 5000, 1000)
			if action == "material.alert_acknowledge" {
				m = countMaterial(t, f, m, f.employee.ID, 1000)
			}
			before := materialSnapshot(t, f)
			if _, err := f.s.db.Exec("CREATE TRIGGER reject_material_audit BEFORE INSERT ON audit_log WHEN NEW.action='" + action + "' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
				t.Fatal(err)
			}
			var err error
			switch action {
			case "material.create":
				_, err = f.s.CreateMaterial(f.ctx, f.admin.ID, materials.Input{Name: "Rollback", Unit: "Liter", Stock: 0, Minimum: 1000})
			case "material.update":
				in := materialUpdate(m)
				in.Minimum = 6000
				_, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
			case "material_check.create", "material.alert_open":
				_, err = f.s.RecordMaterialCheck(f.ctx, f.employee.ID, m.ID, materials.CheckInput{Quantity: 1000, Version: m.Version})
			case "material.alert_acknowledge":
				_, err = f.s.AcknowledgeMaterialAlert(f.ctx, f.supervisor.ID, m.ID, m.OpenAlertID)
			}
			if err == nil {
				t.Fatal("auditfailureignored")
			}
			after := materialSnapshot(t, f)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("partialwrite before=%v after=%v", before, after)
			}
		})
	}
}

func TestMaterialsFiltersAndDashboardFullCount(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 7; i++ {
		_, err := f.s.CreateMaterial(f.ctx, f.admin.ID, materials.Input{Name: fmt.Sprintf("Material %d", i), Unit: "Stück", Stock: 0, Minimum: 1000, Location: "Regal"})
		if err != nil {
			t.Fatal(err)
		}
	}
	d, err := f.s.MaterialDashboard(f.ctx)
	if err != nil || d.OpenCount != 7 || len(d.Alerts) != 5 {
		t.Fatalf("dashboard=%#v,%v", d, err)
	}
	m := materialFixture(t, f, 5000, 1000)
	list, err := f.s.Materials(f.ctx, materials.Filter{Status: materials.FilterStatusLow})
	if err != nil || len(list) != 7 {
		t.Fatalf("lowfilter=%#v,%v", list, err)
	}
	list, err = f.s.Materials(f.ctx, materials.Filter{Search: "sChEiBeN"})
	if err != nil || len(list) != 1 || list[0].ID != m.ID {
		t.Fatalf("search=%#v,%v", list, err)
	}
	in := materialUpdate(m)
	in.Active = false
	m, err = f.s.UpdateMaterial(f.ctx, f.admin.ID, m.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.s.Materials(f.ctx, materials.Filter{Activity: "inaktiv"})
	if err != nil || len(list) != 1 || list[0].ID != m.ID {
		t.Fatalf("inactivefilter=%#v,%v", list, err)
	}
	for _, filter := range []materials.Filter{{Activity: "gelöscht"}, {Status: "falsch"}, {Search: "\x00"}} {
		_, err = f.s.Materials(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
}
