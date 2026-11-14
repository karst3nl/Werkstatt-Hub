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
	"werkstatt-hub/internal/wartung"
)

func maintenanceDate(f fixture, offset int) string {
	return time.Now().In(f.store.TaskSettings().Location).AddDate(0, 0, offset).Format("2006-01-02")
}

func maintenanceObject(t *testing.T, f fixture, name, category string, offset, interval int) wartung.Object {
	t.Helper()
	obj, err := f.store.CreateWartungObject(context.Background(), f.admin.ID, wartung.Input{
		Name: name, Category: category, Location: "Halle 1", IntervalValue: interval,
		IntervalUnit: "tage", LastMaintenanceDate: maintenanceDate(f, offset), Instructions: "Herstellervorgaben beachten.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func maintenanceObjectForm(f fixture) url.Values {
	return url.Values{
		"name": {"Kompressor"}, "description": {"Druckluftversorgung prüfen"}, "category": {"Druckluft"}, "location": {"Technikraum"},
		"interval_value": {"1"}, "interval_unit": {"monate"}, "last_maintenance_date": {maintenanceDate(f, -40)},
		"warning_days": {""}, "instructions": {"Ölstand und Filter prüfen."},
	}
}

func maintenanceRecordForm(f fixture, author core.User, offset int) url.Values {
	return url.Values{"author_id": {strconv.FormatInt(author.ID, 10)}, "performed_date": {maintenanceDate(f, offset)}, "note": {"Filter gereinigt und Anlage geprüft."}, "finding": {"in_ordnung"}}
}

func TestWartungHTTPAdminManagementAndSettings(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	obj := maintenanceObject(t, f, "Hebebühne", "Heben", -40, 30)
	editPath := fmt.Sprintf("/admin/wartung/%d/edit", obj.ID)
	for _, u := range []core.User{f.employee, f.supervisor} {
		b := newBrowser(t, f.handler)
		b.login(u)
		for _, path := range []string{"/admin/wartung/new", editPath, "/admin/wartung/settings"} {
			for _, method := range []string{"GET", "POST"} {
				if w := b.request(method, path, maintenanceObjectForm(f), true, ""); w.Code != http.StatusForbidden {
					t.Fatalf("%s %s %s: %d %s", u.Role, method, path, w.Code, w.Body)
				}
			}
		}
	}
	guest := newBrowser(t, f.handler)
	guest.request("GET", "/", nil, false, "")
	for _, path := range []string{"/admin/wartung/new", editPath, "/admin/wartung/settings"} {
		for _, method := range []string{"GET", "POST"} {
			w := guest.request(method, path, maintenanceObjectForm(f), true, "")
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/login" {
				t.Fatalf("anonymous %s %s: %d %q", method, path, w.Code, w.Header().Get("Location"))
			}
		}
	}
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	for _, path := range []string{"/admin/wartung/new", editPath, "/admin/wartung/settings"} {
		if w := admin.request("GET", path, nil, false, ""); w.Code != http.StatusOK {
			t.Fatalf("admin form %s: %d %s", path, w.Code, w.Body)
		}
	}
	form := maintenanceObjectForm(f)
	form.Set("author_id", strconv.FormatInt(f.employee.ID, 10))
	form.Set("signature_name", "Manipulierte Signatur")
	if w := admin.request("POST", "/admin/wartung/new", form, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("create without CSRF: %d", w.Code)
	}
	if w := admin.request("POST", "/admin/wartung/new", form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	objects, err := f.store.WartungObjects(ctx, wartung.Filter{})
	if err != nil || len(objects) != 2 {
		t.Fatalf("created objects: %d %v", len(objects), err)
	}
	var created wartung.Object
	for _, candidate := range objects {
		if candidate.Name == form.Get("name") {
			created = candidate
		}
	}
	if created.ID == 0 || !created.Active || created.AuthorID != f.admin.ID || created.Signature.Name != f.admin.Name {
		t.Fatalf("creation does not use authenticated admin signature: %#v", created)
	}
	baseline := created.LastMaintenanceDate
	form.Set("version", strconv.FormatInt(created.Version, 10))
	form.Set("active", "1")
	form.Set("interval_value", "2")
	form.Set("last_maintenance_date", "1900-01-01")
	createdEdit := fmt.Sprintf("/admin/wartung/%d/edit", created.ID)
	if w := admin.request("POST", createdEdit, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("edit interval: %d %s", w.Code, w.Body)
	}
	changed, err := f.store.WartungObject(ctx, created.ID)
	if err != nil || changed.IntervalValue != 2 || changed.LastMaintenanceDate != baseline || changed.Signature != created.Signature || changed.Version <= created.Version {
		t.Fatalf("interval edit changed baseline/signature or not version: %#v %v", changed, err)
	}
	if w := admin.request("POST", createdEdit, form, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale object edit: %d %s", w.Code, w.Body)
	}
	settings, err := f.store.WartungSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settingsForm := url.Values{"warning_days": {"14"}, "version": {strconv.FormatInt(settings.Version, 10)}}
	if w := admin.request("POST", "/admin/wartung/settings", settingsForm, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("settings without CSRF: %d", w.Code)
	}
	if w := admin.request("POST", "/admin/wartung/settings", settingsForm, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("settings update: %d %s", w.Code, w.Body)
	}
	updatedSettings, err := f.store.WartungSettings(ctx)
	if err != nil || updatedSettings.WarningDays != 14 || updatedSettings.Version <= settings.Version {
		t.Fatalf("settings did not persist: %#v %v", updatedSettings, err)
	}
	if w := admin.request("POST", "/admin/wartung/settings", settingsForm, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale settings: %d %s", w.Code, w.Body)
	}
	form.Set("version", strconv.FormatInt(changed.Version, 10))
	form.Del("active")
	if w := admin.request("POST", createdEdit, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body)
	}
	inactive, err := f.store.WartungObject(ctx, created.ID)
	if err != nil || inactive.Active {
		t.Fatalf("deactivation failed: %#v %v", inactive, err)
	}
	if w := guest.request("POST", fmt.Sprintf("/wartung/%d/record", created.ID), maintenanceRecordForm(f, f.employee, 0), true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("inactive object accepted record: %d %s", w.Code, w.Body)
	}
	form.Set("version", strconv.FormatInt(inactive.Version, 10))
	form.Set("active", "1")
	if w := admin.request("POST", createdEdit, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("reactivate: %d %s", w.Code, w.Body)
	}
}

func TestWartungHTTPRecordAuthorHistoryAndApproval(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	obj := maintenanceObject(t, f, "Absauganlage", "Lufttechnik", -30, 7)
	otherObj := maintenanceObject(t, f, "Reifenmontiermaschine", "Reifen", -30, 7)
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.other.ID, f.other.Name, f.other.Role, false); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/wartung/%d", obj.ID)
	guest := newBrowser(t, f.handler)
	w := guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), obj.Name) {
		t.Fatalf("public detail: %d %s", w.Code, w.Body)
	}
	authorSelect := regexp.MustCompile(`<select\b[^>]*\bname="author_id"[^>]*>`).FindString(w.Body.String())
	if !strings.Contains(authorSelect, "required") || !strings.Contains(w.Body.String(), `<option value="">`) {
		t.Fatal("maintenance form lacks explicit required author choice")
	}
	before := len(publicAudit(t, f))
	if w = guest.request("POST", path+"/record", maintenanceRecordForm(f, f.employee, -1), false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("record without CSRF: %d", w.Code)
	}
	for _, author := range []string{"", "0", "-1", "abc", "999999", strconv.FormatInt(f.other.ID, 10)} {
		form := maintenanceRecordForm(f, f.employee, -1)
		form.Set("author_id", author)
		if w = guest.request("POST", path+"/record", form, true, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("author %q accepted: %d %s", author, w.Code, w.Body)
		}
	}
	for _, date := range []string{maintenanceDate(f, 1), "2025-02-30", "not-a-date", ""} {
		form := maintenanceRecordForm(f, f.employee, -1)
		form.Set("performed_date", date)
		if w = guest.request("POST", path+"/record", form, true, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("date %q accepted: %d %s", date, w.Code, w.Body)
		}
	}
	if len(publicAudit(t, f)) != before {
		t.Fatal("rejected records wrote audit events")
	}
	form := maintenanceRecordForm(f, f.employee, -1)
	form.Set("signature_name", "Gefälscht")
	form.Set("signature_at", "1900-01-01T00:00:00Z")
	form.Set("approved_by", strconv.FormatInt(f.admin.ID, 10))
	start := time.Now()
	if w = guest.request("POST", path+"/record", form, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != path {
		t.Fatalf("public record: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	history, err := f.store.WartungHistory(ctx, obj.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("history: %d %v", len(history), err)
	}
	record := history[0]
	if record.AuthorID != f.employee.ID || record.Signature.Name != f.employee.Name || record.Signature.At.Before(start) || record.Signature.At.After(time.Now()) || record.Approval != nil {
		t.Fatalf("server record signature invalid: %#v", record)
	}
	if events := publicAudit(t, f); len(events) != before+1 || events[0].ActorID != f.employee.ID {
		t.Fatal("chosen record author does not own audit event")
	}
	if guest.cookies[sessionCookie] != nil {
		t.Fatal("public record issued authenticated session")
	}
	approvalPath := fmt.Sprintf("%s/records/%d/approve", path, record.ID)
	if w = guest.request("POST", approvalPath, url.Values{"author_id": {strconv.FormatInt(f.admin.ID, 10)}}, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous approval: %d %q", w.Code, w.Header().Get("Location"))
	}
	staff := newBrowser(t, f.handler)
	staff.login(f.employee)
	token := staff.cookies[sessionCookie].Value
	if w = staff.request("POST", path+"/record", maintenanceRecordForm(f, f.admin, 0), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("record by chosen other author: %d %s", w.Code, w.Body)
	}
	if staff.cookies[sessionCookie].Value != token {
		t.Fatal("maintenance author selection replaced session")
	}
	if w = staff.request("POST", approvalPath, url.Values{"author_id": {strconv.FormatInt(f.admin.ID, 10)}}, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("chosen admin author granted approval privilege: %d", w.Code)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	if w = manager.request("POST", approvalPath, nil, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("approval without CSRF: %d", w.Code)
	}
	wrongPath := fmt.Sprintf("/wartung/%d/records/%d/approve", otherObj.ID, record.ID)
	if w = manager.request("POST", wrongPath, nil, true, ""); w.Code != http.StatusNotFound {
		t.Fatalf("approval across object boundary: %d %s", w.Code, w.Body)
	}
	if w = manager.request("POST", approvalPath, nil, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("supervisor approval: %d %s", w.Code, w.Body)
	}
	history, err = f.store.WartungHistory(ctx, obj.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("records changed during approval: %d %v", len(history), err)
	}
	var approved wartung.Record
	for _, item := range history {
		if item.ID == record.ID {
			approved = item
		}
	}
	if approved.Approval == nil || approved.Approval.UserID != f.supervisor.ID || approved.Approval.Signature.Name != f.supervisor.Name || approved.Signature != record.Signature {
		t.Fatalf("approval/signature not preserved: %#v", approved)
	}
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	for _, item := range history {
		if item.ID != record.ID {
			if w = admin.request("POST", fmt.Sprintf("%s/records/%d/approve", path, item.ID), nil, true, ""); w.Code != http.StatusSeeOther {
				t.Fatalf("admin approval: %d %s", w.Code, w.Body)
			}
		}
	}
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.employee.ID, "Umbenannter Mitarbeiter", f.employee.Role, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteUser(ctx, f.admin.ID, f.employee.ID); err != nil {
		t.Fatal(err)
	}
	w = guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), record.Signature.Name) || !strings.Contains(w.Body.String(), f.supervisor.Name) || !strings.Contains(w.Body.String(), record.Note) {
		t.Fatalf("public history lost original names or note: %d %s", w.Code, w.Body)
	}
	if w = guest.request("POST", path+"/record", maintenanceRecordForm(f, f.employee, 0), true, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("deleted author accepted: %d %s", w.Code, w.Body)
	}
	for _, suffix := range []string{"edit", "delete"} {
		if w = manager.request("POST", fmt.Sprintf("%s/records/%d/%s", path, record.ID, suffix), nil, true, ""); w.Code != http.StatusNotFound {
			t.Fatalf("append-only history exposes %s: %d", suffix, w.Code)
		}
	}
}

func TestWartungHTTPFiltersDashboardAndLiveWarningDefault(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	overdue := maintenanceObject(t, f, "Feuerlöscher alt", "Brandschutz", -10, 1)
	today := maintenanceObject(t, f, "Bühne heute", "Heben", -1, 1)
	soon := maintenanceObject(t, f, "Kompressor morgen", "Druckluft", 0, 1)
	later := maintenanceObject(t, f, "Beleuchtung später", "Elektrik", 0, 10)
	inactive := maintenanceObject(t, f, "Stillgelegte Absaugung", "Lufttechnik", -20, 1)
	if _, err := f.store.UpdateWartungObject(ctx, f.admin.ID, inactive.ID, wartung.Update{
		Name: inactive.Name, Category: inactive.Category, Location: inactive.Location, IntervalValue: inactive.IntervalValue,
		IntervalUnit: inactive.IntervalUnit, Instructions: inactive.Instructions, Active: false, Version: inactive.Version,
	}); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, f.handler)
	w := b.request("GET", "/wartung", nil, false, "")
	if w.Code != http.StatusOK {
		t.Fatalf("overview: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	previous := -1
	for _, obj := range []wartung.Object{overdue, today, soon, later} {
		position := strings.Index(body, obj.Name)
		if position <= previous {
			t.Fatalf("object %q missing or out of due-date order", obj.Name)
		}
		previous = position
	}
	if strings.Contains(body, inactive.Name) {
		t.Fatal("inactive object appears in default active overview")
	}
	for _, tc := range []struct {
		query  string
		want   []string
		absent []string
	}{
		{"?status=ueberfaellig", []string{overdue.Name}, []string{today.Name, soon.Name, later.Name, inactive.Name}},
		{"?status=bald", []string{today.Name, soon.Name}, []string{overdue.Name, later.Name, inactive.Name}},
		{"?category=Brandschutz", []string{overdue.Name}, []string{today.Name, soon.Name, later.Name}},
		{"?activity=inaktiv", []string{inactive.Name}, []string{overdue.Name, today.Name, soon.Name, later.Name}},
		{"?activity=alle", []string{overdue.Name, today.Name, soon.Name, later.Name, inactive.Name}, nil},
	} {
		w = b.request("GET", "/wartung"+tc.query, nil, false, "")
		if w.Code != http.StatusOK {
			t.Fatalf("filter %s: %d %s", tc.query, w.Code, w.Body)
		}
		for _, name := range tc.want {
			if !strings.Contains(w.Body.String(), name) {
				t.Fatalf("filter %s missing %q", tc.query, name)
			}
		}
		for _, name := range tc.absent {
			if strings.Contains(w.Body.String(), name) {
				t.Fatalf("filter %s unexpectedly includes %q", tc.query, name)
			}
		}
	}
	for _, query := range []string{"?status=unknown", "?activity=unknown"} {
		if w = b.request("GET", "/wartung"+query, nil, false, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid filter %s: %d", query, w.Code)
		}
	}
	w = b.request("GET", "/", nil, false, "")
	body = w.Body.String()
	maintenancePosition := strings.Index(body, "Überfällige Wartungen")
	columnsPosition := strings.Index(body, `class="dashboard-columns"`)
	if w.Code != http.StatusOK || maintenancePosition < 0 || maintenancePosition >= columnsPosition || !strings.Contains(body, overdue.Name) || strings.Contains(body, inactive.Name) {
		t.Fatalf("dashboard overdue warning missing or not above columns: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(body, "/wartung?status=ueberfaellig") || !strings.Contains(body, "/wartung?status=bald") {
		t.Fatal("dashboard lacks filtered maintenance links")
	}
	if !strings.Contains(body, "<strong>1</strong> überfällig") || !strings.Contains(body, "<strong>2</strong> bald fällig") {
		t.Fatal("dashboard maintenance counts include wrong due states or inactive objects")
	}
	settings, err := f.store.WartungSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.UpdateWartungSettings(ctx, f.admin.ID, 14, settings.Version); err != nil {
		t.Fatal(err)
	}
	w = b.request("GET", "/wartung?status=bald", nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), later.Name) {
		t.Fatalf("inherited warning default did not update live: %d %s", w.Code, w.Body)
	}
}

func TestWartungHTTPPendingAdminCannotManageOrApprove(t *testing.T) {
	s, b, initial := bootstrapBrowser(t)
	ctx := context.Background()
	obj, err := s.CreateWartungObject(ctx, initial.User.ID, wartung.Input{Name: "Wartung mit Pflichtwechsel", Category: "Allgemein", IntervalValue: 1, IntervalUnit: "tage", LastMaintenanceDate: "2020-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.RecordWartung(ctx, initial.User.ID, obj.ID, wartung.RecordInput{PerformedDate: "2020-01-02", Note: "Historische Prüfung", Finding: "in_ordnung"})
	if err != nil {
		t.Fatal(err)
	}
	initialLogin(t, b, initial)
	paths := []string{"/admin/wartung/new", fmt.Sprintf("/admin/wartung/%d/edit", obj.ID), "/admin/wartung/settings"}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST"} {
			w := b.request(method, path, url.Values{"warning_days": {"100"}, "version": {"1"}}, true, "")
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/password" {
				t.Fatalf("pending admin %s %s: %d %q", method, path, w.Code, w.Header().Get("Location"))
			}
		}
	}
	approvalPath := fmt.Sprintf("/wartung/%d/records/%d/approve", obj.ID, record.ID)
	if w := b.request("POST", approvalPath, nil, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/password" {
		t.Fatalf("pending admin approved: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, path := range []string{"/wartung", fmt.Sprintf("/wartung/%d", obj.ID), "/"} {
		w := b.request("GET", path, nil, false, "")
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `href="/admin/wartung/new"`) || strings.Contains(w.Body.String(), "Wartung abzeichnen") {
			t.Fatalf("pending public view grants protected action at %s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestWartungHTTPAdvertisedUnicodeTextLimits(t *testing.T) {
	f := setup(t)
	b := newBrowser(t, f.handler)
	b.login(f.admin)
	text := strings.Repeat("🔧", 20000)
	form := maintenanceObjectForm(f)
	form.Set("description", text)
	form.Set("instructions", text)
	if w := b.request("POST", "/admin/wartung/new", form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("valid combined UTF-8 text limits: %d %s", w.Code, w.Body)
	}
	objects, err := f.store.WartungObjects(context.Background(), wartung.Filter{})
	if err != nil || len(objects) != 1 || objects[0].Description != text || objects[0].Instructions != text {
		t.Fatalf("valid long texts were not preserved: objects %d, error %v", len(objects), err)
	}
	before := len(publicAudit(t, f))
	form.Set("instructions", text+"🔧")
	if w := b.request("POST", "/admin/wartung/new", form, true, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("text beyond advertised limit accepted: %d %s", w.Code, w.Body)
	}
	if len(publicAudit(t, f)) != before {
		t.Fatal("invalid long text wrote an audit event")
	}
}
