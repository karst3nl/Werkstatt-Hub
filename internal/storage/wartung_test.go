package storage

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/wartung"
)

func wartungInput() wartung.Input {
	return wartung.Input{Name: "Hebebühne", Category: "Hebetechnik", IntervalValue: 1, IntervalUnit: wartung.UnitMonths, LastMaintenanceDate: "2024-01-31"}
}

func wartungUpdate(object wartung.Object) wartung.Update {
	return wartung.Update{Name: object.Name, Description: object.Description, Category: object.Category,
		Location: object.Location, IntervalValue: object.IntervalValue, IntervalUnit: object.IntervalUnit,
		WarningDays: object.WarningDays, Instructions: object.Instructions, Active: object.Active, Version: object.Version}
}

func (f fixture) wartungObject(t *testing.T) wartung.Object {
	t.Helper()
	object, err := f.s.CreateWartungObject(f.ctx, f.admin.ID, wartungInput())
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func TestWartungAdministrationAndRecordingRights(t *testing.T) {
	f := newFixture(t)
	object := f.wartungObject(t)
	before := len(f.audit(t))
	for _, actor := range []core.User{f.employee, f.supervisor, {ID: 9999}} {
		_, err := f.s.CreateWartungObject(f.ctx, actor.ID, wartungInput())
		assertError(t, err, core.ErrForbidden)
		_, err = f.s.UpdateWartungObject(f.ctx, actor.ID, object.ID, wartungUpdate(object))
		assertError(t, err, core.ErrForbidden)
		_, err = f.s.UpdateWartungSettings(f.ctx, actor.ID, 20, 1)
		assertError(t, err, core.ErrForbidden)
	}
	if len(f.audit(t)) != before {
		t.Fatal("rejected administration changed audit")
	}
	for _, actor := range []core.User{f.employee, f.peer, f.supervisor, f.admin} {
		record, err := f.s.RecordWartung(f.ctx, actor.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-03-15"})
		if err != nil || record.AuthorID != actor.ID || record.Signature.Name != actor.Name || record.Signature.At.IsZero() {
			t.Fatalf("%s maintenance record = %#v, %v", actor.Role, record, err)
		}
	}
	if err := f.s.UpdateUser(f.ctx, f.admin.ID, f.peer.ID, f.peer.Name, f.peer.Role, false); err != nil {
		t.Fatal(err)
	}
	before = len(f.audit(t))
	for _, id := range []int64{0, 9999, f.peer.ID} {
		_, err := f.s.RecordWartung(f.ctx, id, object.ID, wartung.RecordInput{PerformedDate: "2024-03-15"})
		assertError(t, err, core.ErrForbidden)
	}
	if len(f.audit(t)) != before {
		t.Fatal("invalid author changed audit")
	}
}

func TestWartungBackfillIntervalAndAudit(t *testing.T) {
	f := newFixture(t)
	object := f.wartungObject(t)
	due, _ := wartung.NextDue(object.LastMaintenanceDate, object.IntervalValue, object.IntervalUnit)
	if due != "2024-02-29" {
		t.Fatalf("initial due = %s", due)
	}
	record, err := f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-03-15", Note: "  Ölstand geprüft  ", Finding: wartung.FindingDefect})
	if err != nil || record.Note != "Ölstand geprüft" || record.Finding != wartung.FindingDefect || record.Approval != nil {
		t.Fatalf("record = %#v, %v", record, err)
	}
	backfill, err := f.s.RecordWartung(f.ctx, f.peer.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-02-20", Note: "Nachtrag"})
	if err != nil || backfill.Finding != "" {
		t.Fatalf("backfill = %#v, %v", backfill, err)
	}
	stored, err := f.s.WartungObject(f.ctx, object.ID)
	if err != nil || stored.LastMaintenanceDate != "2024-03-15" || stored.Version != object.Version+2 {
		t.Fatalf("backfill rewound last maintenance or version: %#v, %v", stored, err)
	}
	if due, _ = wartung.NextDue(stored.LastMaintenanceDate, stored.IntervalValue, stored.IntervalUnit); due != "2024-04-15" {
		t.Fatalf("actual-date-based due = %s", due)
	}
	_, err = f.s.UpdateWartungObject(f.ctx, f.admin.ID, object.ID, wartungUpdate(object))
	assertError(t, err, core.ErrConflict)
	update := wartungUpdate(stored)
	update.IntervalValue, update.IntervalUnit = 2, wartung.UnitWeeks
	updated, err := f.s.UpdateWartungObject(f.ctx, f.admin.ID, object.ID, update)
	if err != nil || updated.Signature != object.Signature || updated.LastMaintenanceDate != stored.LastMaintenanceDate {
		t.Fatalf("interval update changed history/signature: %#v, %v", updated, err)
	}
	if due, _ = wartung.NextDue(updated.LastMaintenanceDate, updated.IntervalValue, updated.IntervalUnit); due != "2024-03-29" {
		t.Fatalf("interval change did not recalculate due: %s", due)
	}
	history, err := f.s.WartungHistory(f.ctx, object.ID)
	if err != nil || len(history) != 2 || history[0].ID != record.ID || history[1].ID != backfill.ID {
		t.Fatalf("history lost or reordered by submission rather than performance: %#v, %v", history, err)
	}
	event := f.audit(t)[0]
	if event.Action != "wartung.object_update" || event.ActorID != f.admin.ID || event.EntityID != object.ID {
		t.Fatalf("interval audit = %#v", event)
	}
	var oldState, newState wartung.Object
	if err = json.Unmarshal([]byte(event.Before), &oldState); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(event.After), &newState); err != nil {
		t.Fatal(err)
	}
	if oldState.IntervalUnit != wartung.UnitMonths || oldState.IntervalValue != 1 || newState.IntervalUnit != wartung.UnitWeeks || newState.IntervalValue != 2 {
		t.Fatal("interval audit lacks previous/new values")
	}
	before := len(f.audit(t))
	if _, err = f.s.UpdateWartungObject(f.ctx, f.admin.ID, object.ID, wartungUpdate(updated)); err != nil || len(f.audit(t)) != before {
		t.Fatalf("unchanged save created audit: %v", err)
	}
}

func TestWartungDatesUseWorkshopDayAndRejectFuture(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 11, 14, 23, 30, 0, 0, time.UTC) // 15 November in Berlin.
	in := wartungInput()
	in.LastMaintenanceDate = "2026-11-15"
	object, err := f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.audit(t))
	in.LastMaintenanceDate = "2026-11-16"
	_, err = f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now)
	assertError(t, err, core.ErrValidation)
	for _, date := range []string{"2026-11-16", "2026-2-01", "2026-02-30", "0000-01-01", ""} {
		_, err = f.s.recordWartungAt(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: date}, now)
		assertError(t, err, core.ErrValidation)
	}
	if len(f.audit(t)) != before {
		t.Fatal("future or invalid date changed audit")
	}
	if _, err = f.s.recordWartungAt(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2026-11-15"}, now); err != nil {
		t.Fatalf("today in Berlin rejected: %v", err)
	}
	if _, err = f.s.recordWartungAt(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2020-01-01"}, now); err != nil {
		t.Fatalf("historical entry rejected: %v", err)
	}
}

func TestWartungSettingsFiltersDashboardAndInactiveObjects(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	var objects []wartung.Object
	zero := 0
	for _, tc := range []struct {
		name, last, category string
		warning              *int
	}{
		{"überfällig", "2024-01-01", "Hebetechnik", nil},
		{"heute", "2024-02-10", "Hebetechnik", nil},
		{"bald", "2024-02-15", "Luft", nil},
		{"ohne Vorwarnung", "2024-02-15", "Luft", &zero},
		{"später", "2024-03-01", "Luft", nil},
	} {
		in := wartungInput()
		in.Name, in.LastMaintenanceDate, in.Category, in.WarningDays = tc.name, tc.last, tc.category, tc.warning
		object, err := f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now)
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, object)
	}
	settings, err := f.s.WartungSettings(f.ctx)
	if err != nil || settings != wartung.DefaultSettings() {
		t.Fatalf("global defaults = %#v, %v", settings, err)
	}
	list, err := f.s.wartungObjectsAt(f.ctx, wartung.Filter{}, now)
	if err != nil || len(list) != 5 || list[0].ID != objects[0].ID || list[4].ID != objects[4].ID {
		t.Fatalf("due ordering = %#v, %v", list, err)
	}
	soon, err := f.s.wartungObjectsAt(f.ctx, wartung.Filter{Status: wartung.StatusSoon}, now)
	if err != nil || len(soon) != 2 {
		t.Fatalf("soon filter = %#v, %v", soon, err)
	}
	soon, err = f.s.wartungObjectsAt(f.ctx, wartung.Filter{Status: wartung.StatusSoon, Category: "Luft"}, now)
	if err != nil || len(soon) != 1 || soon[0].ID != objects[2].ID {
		t.Fatalf("combined filter = %#v, %v", soon, err)
	}
	dashboard, err := f.s.wartungDashboardAt(f.ctx, now)
	if err != nil || dashboard.OverdueCount != 1 || dashboard.SoonCount != 2 {
		t.Fatalf("dashboard counts = %#v, %v", dashboard, err)
	}
	settings, err = f.s.UpdateWartungSettings(f.ctx, f.admin.ID, 0, settings.Version)
	if err != nil {
		t.Fatal(err)
	}
	soon, err = f.s.wartungObjectsAt(f.ctx, wartung.Filter{Status: wartung.StatusSoon}, now)
	if err != nil || len(soon) != 1 || soon[0].ID != objects[1].ID {
		t.Fatalf("live inheritance or today-withoutwarning failed = %#v, %v", soon, err)
	}
	_, err = f.s.UpdateWartungSettings(f.ctx, f.admin.ID, 20, settings.Version-1)
	assertError(t, err, core.ErrConflict)
	update := wartungUpdate(objects[0])
	update.Active = false
	if _, err = f.s.UpdateWartungObject(f.ctx, f.admin.ID, objects[0].ID, update); err != nil {
		t.Fatal(err)
	}
	dashboard, err = f.s.wartungDashboardAt(f.ctx, now)
	if err != nil || dashboard.OverdueCount != 0 {
		t.Fatalf("inactive object included in dashboard = %#v, %v", dashboard, err)
	}
	inactive, err := f.s.wartungObjectsAt(f.ctx, wartung.Filter{Activity: "inaktiv"}, now)
	if err != nil || len(inactive) != 1 || inactive[0].ID != objects[0].ID {
		t.Fatalf("inactive filter = %#v, %v", inactive, err)
	}
	all, err := f.s.wartungObjectsAt(f.ctx, wartung.Filter{Activity: "alle"}, now)
	if err != nil || len(all) != 5 {
		t.Fatalf("all objects = %#v, %v", all, err)
	}
	_, err = f.s.RecordWartung(f.ctx, f.employee.ID, objects[0].ID, wartung.RecordInput{PerformedDate: "2024-03-01"})
	assertError(t, err, core.ErrForbidden)
	for _, filter := range []wartung.Filter{{Activity: "unknown"}, {Status: "unknown"}, {Category: "\x00"}} {
		_, err = f.s.WartungObjects(f.ctx, filter)
		assertError(t, err, core.ErrValidation)
	}
}

func TestWartungApprovalHistoryAndUserDeletion(t *testing.T) {
	f := newFixture(t)
	object := f.wartungObject(t)
	record, err := f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-03-01", Finding: wartung.FindingOK})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.ApproveWartung(f.ctx, f.employee.ID, object.ID, record.ID)
	assertError(t, err, core.ErrForbidden)
	_, err = f.s.ApproveWartung(f.ctx, f.supervisor.ID, object.ID+1, record.ID)
	assertError(t, err, core.ErrNotFound)
	before := len(f.audit(t))
	var wg sync.WaitGroup
	errorsFound := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.s.ApproveWartung(f.ctx, f.supervisor.ID, object.ID, record.ID)
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	succeeded, conflicted := 0, 0
	for err := range errorsFound {
		if err == nil {
			succeeded++
		} else if errors.Is(err, core.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 7 || len(f.audit(t)) != before+1 {
		t.Fatalf("parallel approval = %d successful, %d conflicts", succeeded, conflicted)
	}
	if err = f.s.UpdateUser(f.ctx, f.admin.ID, f.supervisor.ID, "Neue Leitung", f.supervisor.Role, true); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DeleteUser(f.ctx, f.admin.ID, f.employee.ID); err != nil {
		t.Fatal(err)
	}
	history, err := f.s.WartungHistory(f.ctx, object.ID)
	if err != nil || len(history) != 1 || history[0].Signature != record.Signature || history[0].Approval == nil || history[0].Approval.Signature.Name != f.supervisor.Name {
		t.Fatalf("historical signatures were changed = %#v, %v", history, err)
	}
	_, err = f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-03-01"})
	assertError(t, err, core.ErrForbidden)
	stored, err := f.s.WartungObject(f.ctx, object.ID)
	if err != nil || stored.LastMaintenanceDate != "2024-03-01" {
		t.Fatalf("approval changed last performance date: %#v, %v", stored, err)
	}
}

func TestWartungWritesRollbackOnAuditFailure(t *testing.T) {
	f := newFixture(t)
	object := f.wartungObject(t)
	record, err := f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-02-15"})
	if err != nil {
		t.Fatal(err)
	}
	object, _ = f.s.WartungObject(f.ctx, object.ID)
	settings, _ := f.s.WartungSettings(f.ctx)
	before := len(f.audit(t))
	if _, err = f.s.db.Exec(`CREATE TRIGGER reject_wartung_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT,'simulated audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateWartungObject(f.ctx, f.admin.ID, wartungInput()); err == nil {
		t.Fatal("object creation succeeded without audit")
	}
	update := wartungUpdate(object)
	update.IntervalValue = 4
	if _, err = f.s.UpdateWartungObject(f.ctx, f.admin.ID, object.ID, update); err == nil {
		t.Fatal("object update succeeded without audit")
	}
	if _, err = f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "2024-04-01"}); err == nil {
		t.Fatal("maintenance succeeded without audit")
	}
	if _, err = f.s.ApproveWartung(f.ctx, f.supervisor.ID, object.ID, record.ID); err == nil {
		t.Fatal("approval succeeded without audit")
	}
	if _, err = f.s.UpdateWartungSettings(f.ctx, f.admin.ID, 30, settings.Version); err == nil {
		t.Fatal("settings update succeeded without audit")
	}
	stored, err := f.s.WartungObject(f.ctx, object.ID)
	if err != nil || !reflect.DeepEqual(stored, object) {
		t.Fatalf("audit failure changed object = %#v, %v", stored, err)
	}
	storedSettings, err := f.s.WartungSettings(f.ctx)
	if err != nil || storedSettings != settings {
		t.Fatalf("audit failure changed settings = %#v, %v", storedSettings, err)
	}
	history, err := f.s.WartungHistory(f.ctx, object.ID)
	if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0], record) || len(f.audit(t)) != before {
		t.Fatalf("audit failure changed history = %#v, %v", history, err)
	}
	list, err := f.s.WartungObjects(f.ctx, wartung.Filter{Activity: "alle"})
	if err != nil || len(list) != 1 {
		t.Fatalf("audit failure left created object = %#v, %v", list, err)
	}
}

func TestWartungInvalidValuesAndDateOverflowAreAtomic(t *testing.T) {
	f := newFixture(t)
	before := len(f.audit(t))
	badWarning := 366
	for _, mutate := range []func(*wartung.Input){
		func(in *wartung.Input) { in.Name = " " },
		func(in *wartung.Input) { in.Category = "" },
		func(in *wartung.Input) { in.IntervalValue = 0 },
		func(in *wartung.Input) { in.IntervalValue = 1001 },
		func(in *wartung.Input) { in.IntervalUnit = "stunden" },
		func(in *wartung.Input) { in.WarningDays = &badWarning },
		func(in *wartung.Input) { in.Description = "\x00" },
		func(in *wartung.Input) { in.Location = string([]byte{0xff}) },
		func(in *wartung.Input) { in.Instructions = "\x00" },
	} {
		in := wartungInput()
		mutate(&in)
		_, err := f.s.CreateWartungObject(f.ctx, f.admin.ID, in)
		assertError(t, err, core.ErrValidation)
	}
	if len(f.audit(t)) != before {
		t.Fatal("invalid input created audit events")
	}
	object := f.wartungObject(t)
	for _, in := range []wartung.RecordInput{{PerformedDate: "2024-03-01", Finding: "unknown"}, {PerformedDate: "2024-03-01", Note: "\x00"}} {
		_, err := f.s.RecordWartung(f.ctx, f.employee.ID, object.ID, in)
		assertError(t, err, core.ErrValidation)
	}
	for _, warning := range []int{-1, 366} {
		_, err := f.s.UpdateWartungSettings(f.ctx, f.admin.ID, warning, 1)
		assertError(t, err, core.ErrValidation)
	}
	// A distant test clock exercises the supported upper date boundary without
	// allowing future dates in production. New records and interval changes must
	// validate against the latest actual maintenance, rather than the baseline.
	now := time.Date(9999, 12, 31, 12, 0, 0, 0, time.UTC)
	in := wartungInput()
	in.LastMaintenanceDate, in.IntervalValue, in.IntervalUnit = "9999-12-30", 1, wartung.UnitDays
	object, err := f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.recordWartungAt(f.ctx, f.employee.ID, object.ID, wartung.RecordInput{PerformedDate: "9999-12-31"}, now)
	assertError(t, err, core.ErrValidation)
	update := wartungUpdate(object)
	update.IntervalValue = 2
	_, err = f.s.UpdateWartungObject(f.ctx, f.admin.ID, object.ID, update)
	assertError(t, err, core.ErrValidation)
	history, err := f.s.WartungHistory(f.ctx, object.ID)
	if err != nil || len(history) != 0 {
		t.Fatalf("overflow left a maintenance record: %#v, %v", history, err)
	}
}

func TestWartungDashboardCountsAllObjectsAndLimitsPreviews(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		in := wartungInput()
		in.LastMaintenanceDate = "2024-01-01"
		if _, err := f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now); err != nil {
			t.Fatal(err)
		}
		in.LastMaintenanceDate = "2024-02-10"
		if _, err := f.s.createWartungObjectAt(f.ctx, f.admin.ID, in, now); err != nil {
			t.Fatal(err)
		}
	}
	dashboard, err := f.s.wartungDashboardAt(f.ctx, now)
	if err != nil || dashboard.OverdueCount != 7 || dashboard.SoonCount != 7 || len(dashboard.Overdue) != 5 || len(dashboard.Soon) != 5 {
		t.Fatalf("dashboard counters/previews = %#v, %v", dashboard, err)
	}
}
