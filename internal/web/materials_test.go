package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
	"werkstatt-hub/internal/materials"
)

func materialObject(t *testing.T, f fixture, name string, stock, minimum int64) materials.Material {
	t.Helper()
	m, err := f.store.CreateMaterial(context.Background(), f.admin.ID, materials.Input{Name: name, Unit: "Liter", Location: "Lager", Stock: stock, Minimum: minimum})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func materialForm() url.Values {
	return url.Values{"name": {"Scheibenklar"}, "unit": {"Liter"}, "location": {"Materiallager"}, "stock": {"5,125"}, "minimum": {"1.5"}}
}

func materialCheckForm(m materials.Material, author core.User, quantity string) url.Values {
	return url.Values{"author_id": {strconv.FormatInt(author.ID, 10)}, "quantity": {quantity}, "note": {"Bestand im Lager gezählt."}, "version": {strconv.FormatInt(m.Version, 10)}}
}

func TestMaterialsHTTPAdminManagementAndExactQuantities(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m := materialObject(t, f, "Ölbindemittel", 10000, 2000)
	edit := fmt.Sprintf("/admin/materials/%d/edit", m.ID)
	for _, user := range []core.User{f.employee, f.supervisor} {
		b := newBrowser(t, f.handler)
		b.login(user)
		for _, path := range []string{"/admin/materials/new", edit} {
			for _, method := range []string{"GET", "POST"} {
				if w := b.request(method, path, materialForm(), true, ""); w.Code != http.StatusForbidden {
					t.Fatalf("%s %s %s: %d %s", user.Role, method, path, w.Code, w.Body)
				}
			}
		}
	}
	guest := newBrowser(t, f.handler)
	guest.request("GET", "/materials", nil, false, "")
	for _, path := range []string{"/admin/materials/new", edit} {
		w := guest.request("GET", path, nil, false, "")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/login" {
			t.Fatalf("anonymous administration: %d %q", w.Code, w.Header().Get("Location"))
		}
	}
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	form := materialForm()
	form.Set("author_id", strconv.FormatInt(f.employee.ID, 10))
	form.Set("signature_name", "Manipuliert")
	if w := admin.request("POST", "/admin/materials/new", form, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("create without csrf: %d", w.Code)
	}
	w := admin.request("POST", "/admin/materials/new", form, true, "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create material: %d %s", w.Code, w.Body)
	}
	createdID, err := strconv.ParseInt(strings.TrimPrefix(w.Header().Get("Location"), "/materials/"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.store.Material(ctx, createdID)
	if err != nil || created.Stock != 5125 || created.Minimum != 1500 || created.AuthorID != f.admin.ID || created.Signature.Name != f.admin.Name {
		t.Fatalf("exact quantity and authenticated signature: %#v %v", created, err)
	}
	for _, invalid := range []string{"-1", "NaN", "1e3", "1,0000", "1.2,3", "1000000000", ""} {
		form := materialForm()
		form.Set("stock", invalid)
		if w := admin.request("POST", "/admin/materials/new", form, true, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid quantity %q accepted: %d %s", invalid, w.Code, w.Body)
		}
	}
	form = materialForm()
	form.Set("name", "Umbenannter Lagerartikel")
	form.Set("minimum", "3,75")
	form.Set("version", strconv.FormatInt(created.Version, 10))
	form.Set("active", "1")
	form.Set("unit", "Kiste")
	form.Set("stock", "999")
	editCreated := fmt.Sprintf("/admin/materials/%d/edit", created.ID)
	if w := admin.request("POST", editCreated, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("metadata update: %d %s", w.Code, w.Body)
	}
	changed, err := f.store.Material(ctx, created.ID)
	if err != nil || changed.Name != form.Get("name") || changed.Minimum != 3750 || changed.Stock != 5125 || changed.Unit != created.Unit || changed.Signature != created.Signature {
		t.Fatalf("metadata changed measurement/unit/signature: %#v %v", changed, err)
	}
	if w := admin.request("POST", editCreated, form, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale metadata accepted: %d", w.Code)
	}
	form.Set("version", strconv.FormatInt(changed.Version, 10))
	form.Del("active")
	if w := admin.request("POST", editCreated, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("deactivation: %d %s", w.Code, w.Body)
	}
	inactive, _ := f.store.Material(ctx, created.ID)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/materials", false},
		{"/materials?activity=inaktiv", true},
		{"/materials?activity=alle", true},
	} {
		w := guest.request("GET", tc.path, nil, false, "")
		listed := strings.Contains(w.Body.String(), fmt.Sprintf(`href="/materials/%d"`, created.ID))
		if w.Code != http.StatusOK || listed != tc.want {
			t.Fatalf("activity filter %s: listed=%t want=%t status=%d", tc.path, listed, tc.want, w.Code)
		}
	}
	checkPath := fmt.Sprintf("/materials/%d/checks", created.ID)
	if w := guest.request("POST", checkPath, materialCheckForm(inactive, f.employee, "2"), true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("inactive accepted check: %d %s", w.Code, w.Body)
	}
	form.Set("version", strconv.FormatInt(inactive.Version, 10))
	form.Set("active", "1")
	if w := admin.request("POST", editCreated, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("reactivation: %d %s", w.Code, w.Body)
	}
}

func TestMaterialsHTTPStockWarningCycleAndManagerAcknowledgment(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m := materialObject(t, f, "Warnartikel", 4000, 2000)
	other := materialObject(t, f, "Anderer Artikel", 0, 1000)
	path := fmt.Sprintf("/materials/%d", m.ID)
	checkPath := path + "/checks"
	guest := newBrowser(t, f.handler)
	w := guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="author_id"`) || strings.Contains(w.Body.String(), fmt.Sprintf(`value="%d" selected`, f.employee.ID)) {
		t.Fatalf("public author selection: %d %s", w.Code, w.Body)
	}
	form := materialCheckForm(m, f.employee, "2")
	if w := guest.request("POST", checkPath, form, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("check without csrf: %d", w.Code)
	}
	for _, author := range []string{"", "999999"} {
		form.Set("author_id", author)
		if w := guest.request("POST", checkPath, form, true, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid author %q: %d %s", author, w.Code, w.Body)
		}
	}
	form.Set("author_id", strconv.FormatInt(f.employee.ID, 10))
	form.Set("signature_name", "Falscher Ersteller")
	if w := guest.request("POST", checkPath, form, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("stock check: %d %s", w.Code, w.Body)
	}
	if guest.cookies[sessionCookie] != nil {
		t.Fatal("author selection issued session")
	}
	checks, err := f.store.MaterialChecks(ctx, m.ID)
	if err != nil || len(checks) != 1 || checks[0].Quantity != 2000 || checks[0].AuthorID != f.employee.ID || checks[0].Signature.Name != f.employee.Name {
		t.Fatalf("check signature/quantity: %#v %v", checks, err)
	}
	if w := guest.request("POST", checkPath, form, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("stale count accepted: %d %s", w.Code, w.Body)
	}
	alerts, err := f.store.MaterialAlerts(ctx, m.ID)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("minimum equality did not open warning: %#v %v", alerts, err)
	}
	ackPath := fmt.Sprintf("%s/alerts/%d/ack", path, alerts[0].ID)
	staff := newBrowser(t, f.handler)
	staff.login(f.employee)
	if w := staff.request("POST", ackPath, nil, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("employee acknowledged warning: %d", w.Code)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	if w := manager.request("POST", ackPath, nil, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("ack without csrf: %d", w.Code)
	}
	otherAlerts, _ := f.store.MaterialAlerts(ctx, other.ID)
	wrongScope := fmt.Sprintf("%s/alerts/%d/ack", path, otherAlerts[0].ID)
	if w := manager.request("POST", wrongScope, nil, true, ""); w.Code != http.StatusNotFound {
		t.Fatalf("wrong material warning accepted: %d %s", w.Code, w.Body)
	}
	// Replenishment must not silently clear an unacknowledged order warning.
	current, _ := f.store.Material(ctx, m.ID)
	if w := guest.request("POST", checkPath, materialCheckForm(current, f.employee, "8"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("replenish: %d %s", w.Code, w.Body)
	}
	dashboard, err := f.store.MaterialDashboard(ctx)
	if err != nil || dashboard.OpenCount != 2 {
		t.Fatalf("replenishment hid pending warning: %#v %v", dashboard, err)
	}
	for _, tc := range []struct {
		path          string
		want, exclude int64
	}{
		{"/materials?status=niedrig", other.ID, m.ID},
		{"/materials?search=Warnartikel", m.ID, other.ID},
	} {
		w := guest.request("GET", tc.path, nil, false, "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), fmt.Sprintf(`href="/materials/%d"`, tc.want)) || strings.Contains(w.Body.String(), fmt.Sprintf(`href="/materials/%d"`, tc.exclude)) {
			t.Fatalf("material filter %s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if w := manager.request("POST", ackPath, url.Values{"return_to": {"dashboard"}, "author_id": {strconv.FormatInt(f.admin.ID, 10)}}, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("manager ack: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	if w := manager.request("POST", ackPath, nil, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("duplicate ack: %d %s", w.Code, w.Body)
	}
	alerts, _ = f.store.MaterialAlerts(ctx, m.ID)
	if alerts[0].Acknowledgment == nil || alerts[0].Acknowledgment.UserID != f.supervisor.ID || alerts[0].Acknowledgment.Signature.Name != f.supervisor.Name {
		t.Fatalf("ack does not use session reviewer: %#v", alerts[0])
	}
	// A new decline creates exactly one new warning, repeated measurements do not.
	for _, quantity := range []string{"2", "2", "1,5"} {
		current, _ = f.store.Material(ctx, m.ID)
		if w := guest.request("POST", checkPath, materialCheckForm(current, f.employee, quantity), true, ""); w.Code != http.StatusSeeOther {
			t.Fatalf("warning cycle quantity %s: %d %s", quantity, w.Code, w.Body)
		}
	}
	alerts, _ = f.store.MaterialAlerts(ctx, m.ID)
	if len(alerts) != 2 {
		t.Fatalf("repeated low checks duplicated warning: %#v", alerts)
	}
	var openID int64
	for _, alert := range alerts {
		if alert.Acknowledgment == nil {
			openID = alert.ID
		}
	}
	if openID == 0 {
		t.Fatal("new warning is not pending")
	}
	if w := manager.request("POST", fmt.Sprintf("%s/alerts/%d/ack", path, openID), url.Values{"return_to": {"https://example.invalid"}}, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != path {
		t.Fatalf("ack redirect was not scoped to material: %d %q", w.Code, w.Header().Get("Location"))
	}
	current, _ = f.store.Material(ctx, m.ID)
	if w := guest.request("POST", checkPath, materialCheckForm(current, f.employee, "1,5"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("same low measurement after acknowledgment: %d %s", w.Code, w.Body)
	}
	alerts, _ = f.store.MaterialAlerts(ctx, m.ID)
	if len(alerts) != 2 {
		t.Fatalf("same low count reopened acknowledged warning: %#v", alerts)
	}
	current, _ = f.store.Material(ctx, m.ID)
	if w := guest.request("POST", checkPath, materialCheckForm(current, f.employee, "1"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("further decline after acknowledgment: %d %s", w.Code, w.Body)
	}
	alerts, _ = f.store.MaterialAlerts(ctx, m.ID)
	if len(alerts) != 3 {
		t.Fatalf("later decline did not create new warning: %#v", alerts)
	}
	// The author of a check must never replace a logged-in staff identity.
	oldSession := staff.cookies[sessionCookie].Value
	current, _ = f.store.Material(ctx, m.ID)
	if w := staff.request("POST", checkPath, materialCheckForm(current, f.admin, "1,5"), true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("explicit different author: %d %s", w.Code, w.Body)
	}
	if staff.cookies[sessionCookie].Value != oldSession || staff.request("GET", "/admin/users", nil, false, "").Code != http.StatusForbidden {
		t.Fatal("selected author elevated staff session")
	}
	w = guest.request("GET", "/", nil, false, "")
	html := w.Body.String()
	materialBlock, maintenanceBlock, columns := strings.Index(html, `aria-label="Verbrauchsmaterialien"`), strings.Index(html, `aria-label="Wartung"`), strings.Index(html, `class="dashboard-columns"`)
	if w.Code != http.StatusOK || materialBlock < 0 || materialBlock > maintenanceBlock || maintenanceBlock > columns || !strings.Contains(html, "sollte bestellt werden") {
		t.Fatalf("dashboard warning placement: %d %s", w.Code, w.Body)
	}
	admin := newBrowser(t, f.handler)
	admin.login(f.admin)
	current, _ = f.store.Material(ctx, other.ID)
	metadata := url.Values{"name": {current.Name}, "location": {current.Location}, "minimum": {materials.FormatQuantity(current.Minimum)}, "version": {strconv.FormatInt(current.Version, 10)}}
	if w := admin.request("POST", fmt.Sprintf("/admin/materials/%d/edit", other.ID), metadata, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("deactivate pending material: %d %s", w.Code, w.Body)
	}
	dashboard, _ = f.store.MaterialDashboard(ctx)
	if dashboard.OpenCount != 1 {
		t.Fatalf("inactive material still in dashboard: %#v", dashboard)
	}
	current, _ = f.store.Material(ctx, other.ID)
	metadata.Set("active", "1")
	metadata.Set("version", strconv.FormatInt(current.Version, 10))
	if w := admin.request("POST", fmt.Sprintf("/admin/materials/%d/edit", other.ID), metadata, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("reactivate pending material: %d %s", w.Code, w.Body)
	}
	dashboard, _ = f.store.MaterialDashboard(ctx)
	if dashboard.OpenCount != 2 {
		t.Fatalf("reactivation lost pending warning: %#v", dashboard)
	}
	retained, _ := f.store.MaterialAlerts(ctx, other.ID)
	if len(retained) != 1 || retained[0].ID != otherAlerts[0].ID || retained[0].Acknowledgment != nil {
		t.Fatalf("deactivation changed warning history: %#v", retained)
	}
}

func TestMaterialsHTTPCheckPrioritiesCommentsAndPreservedHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m := materialObject(t, f, "Historisches Material", 6000, 1000)
	check, err := f.store.RecordMaterialCheck(ctx, f.other.ID, m.ID, materials.CheckInput{Quantity: 5000, Note: "Historische Bestandsprüfung", Version: m.Version})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/material-checks/%d", check.ID)
	guest := newBrowser(t, f.handler)
	w := guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || strings.Count(w.Body.String(), `class="wrench-glyph"`) != 10 || !strings.Contains(w.Body.String(), f.other.Name) {
		t.Fatalf("public check lacks priorities/signature: %d %s", w.Code, w.Body)
	}
	if w := guest.request("POST", path+"/comments", url.Values{"body": {"Gastkommentar"}}, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("anonymous comment did not require name: %d", w.Code)
	}
	staff := newBrowser(t, f.handler)
	staff.login(f.employee)
	if w := staff.request("POST", path+"/comments", url.Values{"body": {"Bitte nächste Lieferung prüfen."}}, false, ""); w.Code != http.StatusForbidden {
		t.Fatalf("comment without csrf: %d", w.Code)
	}
	if w := staff.request("POST", path+"/comments", url.Values{"body": {"Bitte nächste Lieferung prüfen."}}, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("staff comment: %d %s", w.Code, w.Body)
	}
	priority := url.Values{"side": {"mitarbeiter"}, "value": {"4"}, "version": {strconv.FormatInt(check.Version, 10)}}
	if w := staff.request("POST", path+"/priority", priority, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("staff priority: %d %s", w.Code, w.Body)
	}
	changed, _ := f.store.MaterialCheck(ctx, check.ID)
	priority.Set("side", "vorgesetzter")
	priority.Set("version", strconv.FormatInt(changed.Version, 10))
	if w := staff.request("POST", path+"/priority", priority, true, ""); w.Code != http.StatusForbidden {
		t.Fatalf("staff supervisor priority: %d", w.Code)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	priority.Set("value", "5")
	if w := manager.request("POST", path+"/priority", priority, true, ""); w.Code != http.StatusSeeOther {
		t.Fatalf("supervisor priority: %d %s", w.Code, w.Body)
	}
	idea, err := f.store.CreateIdea(ctx, f.employee.ID, ideas.Input{Title: "Anderes Modul", Body: "Nicht über Materialrouten bearbeiten", Category: "Allgemein"})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/comments", "/priority"} {
		form := url.Values{"body": {"Falscher Bereich"}, "side": {"mitarbeiter"}, "value": {"3"}, "version": {strconv.FormatInt(idea.Version, 10)}}
		if w := staff.request("POST", fmt.Sprintf("/material-checks/%d%s", idea.ID, suffix), form, true, ""); w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
			t.Fatalf("cross-module route %s: %d %s", suffix, w.Code, w.Body)
		}
	}
	comments, err := f.store.PublicComments(ctx, idea.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("cross-module comment changed other entry: %#v %v", comments, err)
	}
	unchangedIdea, err := f.store.PublicIdea(ctx, idea.ID)
	if err != nil || unchangedIdea.EmployeePriority != nil || unchangedIdea.Version != idea.Version {
		t.Fatalf("cross-module priority changed other entry: %#v %v", unchangedIdea, err)
	}
	if err := f.store.UpdateUser(ctx, f.admin.ID, f.other.ID, "Neuer Name", f.other.Role, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteUser(ctx, f.admin.ID, f.other.ID); err != nil {
		t.Fatal(err)
	}
	w = guest.request("GET", path, nil, false, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), f.other.Name) || !strings.Contains(w.Body.String(), "Bitte nächste Lieferung prüfen.") {
		t.Fatalf("historical signature or comment lost: %d %s", w.Code, w.Body)
	}
	final, err := f.store.MaterialCheck(ctx, check.ID)
	if err != nil || final.Quantity != check.Quantity || final.Signature != check.Signature || final.EmployeePriority == nil || *final.EmployeePriority != 4 || final.SupervisorPriority == nil || *final.SupervisorPriority != 5 {
		t.Fatalf("priorities changed measurement/history: %#v %v", final, err)
	}
	for _, suffix := range []string{"/edit", "/delete"} {
		if w := manager.request("POST", path+suffix, nil, true, ""); w.Code != http.StatusNotFound {
			t.Fatalf("immutable measurement route exists %s: %d", suffix, w.Code)
		}
	}
}

func TestMaterialsHTTPPendingAdminProtectedActions(t *testing.T) {
	s, b, initial := bootstrapBrowser(t)
	ctx := context.Background()
	m, err := s.CreateMaterial(ctx, initial.User.ID, materials.Input{Name: "Erstinstallation", Unit: "Kiste", Stock: 0, Minimum: 1000})
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := s.MaterialAlerts(ctx, m.ID)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("initial warning missing: %#v %v", alerts, err)
	}
	initialLogin(t, b, initial)
	for _, path := range []string{"/admin/materials/new", fmt.Sprintf("/admin/materials/%d/edit", m.ID)} {
		for _, method := range []string{"GET", "POST"} {
			w := b.request(method, path, materialForm(), true, "")
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/password" {
				t.Fatalf("pending admin %s %s: %d %q", method, path, w.Code, w.Header().Get("Location"))
			}
		}
	}
	ack := fmt.Sprintf("/materials/%d/alerts/%d/ack", m.ID, alerts[0].ID)
	if w := b.request("POST", ack, nil, true, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/password" {
		t.Fatalf("pending admin acknowledged warning: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, path := range []string{"/materials", fmt.Sprintf("/materials/%d", m.ID), "/"} {
		w := b.request("GET", path, nil, false, "")
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `action="`+ack+`"`) || strings.Contains(w.Body.String(), `href="/admin/materials/new"`) {
			t.Fatalf("pending public view grants administration at %s: %d %s", path, w.Code, w.Body)
		}
	}
}
