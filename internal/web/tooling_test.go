package web

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tooling"
)

func toolingFixtureTool(t *testing.T, f fixture, name string) tooling.Tool {
	t.Helper()
	tool, err := f.store.CreateTool(context.Background(), f.admin.ID, tooling.ToolInput{Name: name, Category: "Handwerkzeug", Location: "Werkstattwagen", Condition: "ok", AuthorID: f.admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}
func toolingID(id int64) string { return strconv.FormatInt(id, 10) }

func TestToolHTTPInventoryRequiresManagerAndKeepsChosenSignature(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	anon := newBrowser(t, f.handler)
	if w := anon.request("GET", "/tools", nil, false, ""); w.Code != 200 {
		t.Fatalf("public inventory: %d %s", w.Code, w.Body)
	}
	form := url.Values{"author_id": {toolingID(f.admin.ID)}, "name": {"<script>Zange</script>"}, "category": {"Handwerkzeug"}, "location": {"Werkstattwagen"}, "condition": {"ok"}}
	if w := anon.request("GET", "/tools/new", nil, false, ""); w.Code != 303 {
		t.Fatalf("anonymous inventory form: %d", w.Code)
	}
	if w := anon.request("POST", "/tools/new", form, true, ""); w.Code != 303 {
		t.Fatalf("anonymous inventory creation: %d", w.Code)
	}
	employee := newBrowser(t, f.handler)
	employee.login(f.employee)
	if w := employee.request("POST", "/tools/new", form, true, ""); w.Code != 403 {
		t.Fatalf("employee creates official inventory: %d %s", w.Code, w.Body)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	sessionBefore := manager.cookies[sessionCookie].Value
	if w := manager.request("GET", "/tools/new", nil, false, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `name="author_id"`) {
		t.Fatalf("manager tool form: %d %s", w.Code, w.Body)
	}
	form.Del("csrf")
	if w := manager.request("POST", "/tools/new", form, false, ""); w.Code != 403 {
		t.Fatalf("tool creation without csrf: %d", w.Code)
	}
	form.Set("author_id", toolingID(f.employee.ID))
	if w := manager.request("POST", "/tools/new", form, true, ""); w.Code != 400 {
		t.Fatalf("nonmanager tool author: %d %s", w.Code, w.Body)
	}
	form.Set("author_id", toolingID(f.admin.ID))
	form.Set("signature_name", "Falscher Name")
	form.Set("signature_at", "1900-01-01")
	start := time.Now()
	if w := manager.request("POST", "/tools/new", form, true, ""); w.Code != 303 {
		t.Fatalf("manager creates tool: %d %s", w.Code, w.Body)
	}
	all, err := f.store.Tools(ctx, tooling.ToolFilter{})
	if err != nil || len(all) != 1 {
		t.Fatalf("inventory: %#v %v", all, err)
	}
	tool := all[0]
	if tool.AuthorID != f.admin.ID || tool.Signature.Name != f.admin.Name || tool.Signature.At.Before(start) {
		t.Fatalf("tool signature not selected server identity: %#v", tool)
	}
	if manager.cookies[sessionCookie].Value != sessionBefore {
		t.Fatal("chosen author replaced manager session")
	}
	audit, err := f.store.Audit(ctx, f.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if audit[0].ActorID != f.supervisor.ID || audit[0].Action != "tool.create" {
		t.Fatalf("actual manager missing from audit: %#v", audit[0])
	}
	path := fmt.Sprintf("/tools/%d", tool.ID)
	if w := anon.request("GET", path, nil, false, ""); w.Code != 200 || strings.Contains(w.Body.String(), "<script>Zange</script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("public tool escaped detail: %d %s", w.Code, w.Body)
	}
	update := url.Values{"name": {"Zange"}, "category": {"Handwerkzeug"}, "location": {"Werkbank"}, "condition": {"defekt"}, "version": {toolingID(tool.Version)}}
	if w := employee.request("POST", path+"/edit", update, true, ""); w.Code != 403 {
		t.Fatalf("employee changes official condition: %d %s", w.Code, w.Body)
	}
	if w := manager.request("POST", path+"/edit", update, true, ""); w.Code != 303 {
		t.Fatalf("manager changes condition: %d %s", w.Code, w.Body)
	}
	if w := manager.request("POST", path+"/edit", update, true, ""); w.Code != 409 {
		t.Fatalf("stale tool edit: %d %s", w.Code, w.Body)
	}
	got, err := f.store.Tool(ctx, tool.ID)
	if err != nil || got.Condition != "defekt" || got.Location != "Werkbank" || got.Signature != tool.Signature {
		t.Fatalf("tool edit/signature: %#v %v", got, err)
	}
	if w := anon.request("GET", "/tools?condition=ok", nil, false, ""); w.Code != 200 || strings.Contains(w.Body.String(), ">Zange</a>") {
		t.Fatalf("inventory condition filter: %d %s", w.Code, w.Body)
	}
	if w := anon.request("GET", "/tools?condition=defekt&search=Zange", nil, false, ""); w.Code != 200 || !strings.Contains(w.Body.String(), ">Zange</a>") {
		t.Fatalf("inventory search and condition filter: %d %s", w.Code, w.Body)
	}
	if w := anon.request("GET", path, nil, false, ""); w.Code != 200 || strings.Contains(w.Body.String(), "bootstrap_admin") || strings.Contains(w.Body.String(), "Nutzer angelegt") {
		t.Fatalf("tool history leaked unrelated administration: %d %s", w.Code, w.Body)
	}
}

func TestToolReportHTTPPublicCreationAndOfficialConditionBoundary(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	tool := toolingFixtureTool(t, f, "Drehmomentschlüssel")
	anon := newBrowser(t, f.handler)
	newPath := fmt.Sprintf("/tools/%d/reports/new", tool.ID)
	if w := anon.request("GET", newPath, nil, false, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `name="author_id"`) {
		t.Fatalf("public report form: %d %s", w.Code, w.Body)
	}
	form := url.Values{"author_id": {toolingID(f.employee.ID)}, "title": {"<script>Defektmeldung</script>"}, "body": {"Ratsche blockiert"}, "observed_condition": {"defekt"}, "signature_name": {"Fremder Name"}, "status": {"erledigt"}}
	if w := anon.request("POST", newPath, form, false, ""); w.Code != 403 {
		t.Fatalf("report without csrf: %d", w.Code)
	}
	if w := anon.request("POST", newPath, form, true, ""); w.Code != 303 {
		t.Fatalf("public report: %d %s", w.Code, w.Body)
	}
	reports, err := f.store.ToolReports(ctx, tooling.ReportFilter{ToolID: tool.ID})
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports: %#v %v", reports, err)
	}
	report := reports[0]
	if report.AuthorID != f.employee.ID || report.Signature.Name != f.employee.Name || report.Status != "neu" {
		t.Fatalf("report author/status: %#v", report)
	}
	if anon.cookies[sessionCookie] != nil {
		t.Fatal("anonymous report issued a session")
	}
	storedTool, err := f.store.Tool(ctx, tool.ID)
	if err != nil || storedTool.Condition != "ok" || storedTool.OpenReports != 1 {
		t.Fatalf("report silently changed official condition: %#v %v", storedTool, err)
	}
	path := fmt.Sprintf("/tool-reports/%d", report.ID)
	if w := anon.request("GET", path, nil, false, ""); w.Code != 200 || strings.Contains(w.Body.String(), "<script>Defektmeldung</script>") {
		t.Fatalf("report detail escaping: %d %s", w.Code, w.Body)
	}
	if w := anon.request("GET", fmt.Sprintf("/entries/%d", report.ID), nil, false, ""); w.Code != 303 || w.Header().Get("Location") != path {
		t.Fatalf("report canonical redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	author := newBrowser(t, f.handler)
	author.login(f.employee)
	edit := url.Values{"title": {"Ratsche prüfen"}, "body": {report.Body}, "observed_condition": {"verschleisst"}, "status": {"erledigt"}, "version": {toolingID(report.Version)}}
	if w := author.request("POST", path+"/edit", edit, true, ""); w.Code != 403 {
		t.Fatalf("report author resolves own report: %d %s", w.Code, w.Body)
	}
	edit.Del("status")
	if w := author.request("POST", path+"/edit", edit, true, ""); w.Code != 303 {
		t.Fatalf("report author text/observation edit: %d %s", w.Code, w.Body)
	}
	if w := author.request("POST", path+"/edit", edit, true, ""); w.Code != 409 {
		t.Fatalf("stale report edit: %d %s", w.Code, w.Body)
	}
	peer := newBrowser(t, f.handler)
	peer.login(f.other)
	if w := peer.request("GET", path+"/edit", nil, false, ""); w.Code != 403 {
		t.Fatalf("peer report edit: %d", w.Code)
	}
	report, err = f.store.ToolReport(ctx, report.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	edit.Set("status", "erledigt")
	edit.Set("version", toolingID(report.Version))
	if w := manager.request("POST", path+"/edit", edit, true, ""); w.Code != 303 {
		t.Fatalf("manager resolves report: %d %s", w.Code, w.Body)
	}
	storedTool, err = f.store.Tool(ctx, tool.ID)
	if err != nil || storedTool.Condition != "ok" || storedTool.OpenReports != 0 {
		t.Fatalf("resolved report boundary: %#v %v", storedTool, err)
	}
	if w := manager.request("POST", fmt.Sprintf("/entries/%d/edit", report.ID), url.Values{"title": {"Bypass"}, "body": {"Nicht direkt ändern"}, "status": {"neu"}, "version": {toolingID(report.Version + 1)}}, true, ""); w.Code == 303 || w.Code == 200 {
		t.Fatalf("generic report edit bypass: %d", w.Code)
	}
}

func TestPurchaseHTTPWorkflowAuthorRightsAndNoAutomaticInventory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	tool := toolingFixtureTool(t, f, "Alter Diagnosetester")
	anon := newBrowser(t, f.handler)
	if w := anon.request("GET", "/purchases/new", nil, false, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `name="author_id"`) {
		t.Fatalf("public purchase form: %d %s", w.Code, w.Body)
	}
	form := url.Values{"author_id": {toolingID(f.employee.ID)}, "title": {"Neuer Diagnosetester"}, "body": {"Aktuelle Fahrzeuge unterstützen"}, "category": {"Diagnose"}, "location": {"Diagnoseplatz"}, "tool_id": {toolingID(tool.ID)}, "status": {"geliefert"}, "signature_name": {"Admin"}}
	if w := anon.request("POST", "/purchases/new", form, true, ""); w.Code != 303 {
		t.Fatalf("public purchase wish: %d %s", w.Code, w.Body)
	}
	all, err := f.store.Purchases(ctx, tooling.PurchaseFilter{})
	if err != nil || len(all) != 1 {
		t.Fatalf("purchases: %#v %v", all, err)
	}
	purchase := all[0]
	if purchase.Status != "gewuenscht" || purchase.Signature.Name != f.employee.Name || purchase.ToolID != tool.ID || anon.cookies[sessionCookie] != nil {
		t.Fatalf("purchase initial identity/status: %#v", purchase)
	}
	path := fmt.Sprintf("/purchases/%d", purchase.ID)
	if w := anon.request("GET", path, nil, false, ""); w.Code != 200 {
		t.Fatalf("public purchase detail: %d %s", w.Code, w.Body)
	}
	author := newBrowser(t, f.handler)
	author.login(f.employee)
	edit := url.Values{"title": {"Diagnosetester erneuern"}, "body": {purchase.Body}, "category": {"Diagnosegeräte"}, "location": {"Halle 2"}, "status": {"genehmigt"}, "version": {toolingID(purchase.Version)}}
	if w := author.request("POST", path+"/edit", edit, true, ""); w.Code != 403 {
		t.Fatalf("employee approves purchase: %d %s", w.Code, w.Body)
	}
	edit.Del("status")
	if w := author.request("POST", path+"/edit", edit, true, ""); w.Code != 303 {
		t.Fatalf("employee edits own pending wish: %d %s", w.Code, w.Body)
	}
	peer := newBrowser(t, f.handler)
	peer.login(f.other)
	if w := peer.request("GET", path+"/edit", nil, false, ""); w.Code != 403 {
		t.Fatalf("peer edits purchase: %d", w.Code)
	}
	manager := newBrowser(t, f.handler)
	manager.login(f.supervisor)
	for _, status := range []string{"genehmigt", "bestellt", "geliefert"} {
		current, err := f.store.Purchase(ctx, purchase.ID)
		if err != nil {
			t.Fatal(err)
		}
		edit.Set("status", status)
		edit.Set("version", toolingID(current.Version))
		edit.Set("tool_id", "999999")
		if w := manager.request("POST", path+"/edit", edit, true, ""); w.Code != 303 {
			t.Fatalf("manager purchase status %s: %d %s", status, w.Code, w.Body)
		}
	}
	if w := author.request("GET", path+"/edit", nil, false, ""); w.Code != 403 {
		t.Fatalf("author edits progressed purchase: %d %s", w.Code, w.Body)
	}
	current, err := f.store.Purchase(ctx, purchase.ID)
	if err != nil || current.Status != "geliefert" || current.ToolID != tool.ID || current.Signature != purchase.Signature {
		t.Fatalf("purchase immutable fields: %#v %v", current, err)
	}
	inventory, err := f.store.Tools(ctx, tooling.ToolFilter{})
	if err != nil || len(inventory) != 1 {
		t.Fatalf("delivery automatically created inventory: %#v %v", inventory, err)
	}
	if w := anon.request("GET", fmt.Sprintf("/entries/%d", purchase.ID), nil, false, ""); w.Code != 303 || w.Header().Get("Location") != path {
		t.Fatalf("purchase canonical redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestToolingHTTPAuthorValidationAndSessionIsNotReplaced(t *testing.T) {
	for _, kind := range []string{"report", "purchase"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			tool := toolingFixtureTool(t, f, "Testwerkzeug")
			if err := f.store.UpdateUser(ctx, f.admin.ID, f.other.ID, f.other.Name, f.other.Role, false); err != nil {
				t.Fatal(err)
			}
			path := "/purchases/new"
			if kind == "report" {
				path = fmt.Sprintf("/tools/%d/reports/new", tool.ID)
			}
			anon := newBrowser(t, f.handler)
			anon.request("GET", path, nil, false, "")
			auditBefore, err := f.store.Audit(ctx, f.admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, author := range []string{"", "999999", toolingID(f.other.ID)} {
				form := url.Values{"author_id": {author}, "title": {"Kein gültiger Autor"}, "body": {"Nicht speichern"}, "category": {"Allgemein"}, "observed_condition": {"defekt"}}
				if w := anon.request("POST", path, form, true, ""); w.Code != 400 {
					t.Fatalf("author %q: %d %s", author, w.Code, w.Body)
				}
			}
			auditAfter, err := f.store.Audit(ctx, f.admin.ID)
			if err != nil || len(auditAfter) != len(auditBefore) {
				t.Fatalf("invalid author left audit: %v", err)
			}
			logged := newBrowser(t, f.handler)
			logged.login(f.employee)
			tokenBefore := logged.cookies[sessionCookie].Value
			form := url.Values{"author_id": {toolingID(f.admin.ID)}, "title": {"Admin ist gewählter Ersteller"}, "body": {"Die Namenssitzung bleibt Mitarbeiter"}, "category": {"Allgemein"}, "observed_condition": {"defekt"}}
			if w := logged.request("POST", path, form, true, ""); w.Code != 303 {
				t.Fatalf("selected author create: %d %s", w.Code, w.Body)
			}
			if logged.cookies[sessionCookie].Value != tokenBefore {
				t.Fatal("author selection replaced session")
			}
			if w := logged.request("GET", "/admin/users", nil, false, ""); w.Code != 403 {
				t.Fatalf("selected admin author granted privileges: %d", w.Code)
			}
		})
	}
}

func TestToolingPriorityHTTPUsesIndependentSidesAndVersions(t *testing.T) {
	for _, kind := range []string{"report", "purchase"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			tool := toolingFixtureTool(t, f, "Testwerkzeug")
			var entry core.Entry
			var path string
			if kind == "report" {
				report, err := f.store.CreateToolReport(ctx, f.employee.ID, tooling.ReportInput{ToolID: tool.ID, Title: "Meldung", Body: "Prüfen", ObservedCondition: "defekt"})
				if err != nil {
					t.Fatal(err)
				}
				entry = report.Entry
				path = fmt.Sprintf("/tool-reports/%d", entry.ID)
			} else {
				purchase, err := f.store.CreatePurchase(ctx, f.employee.ID, tooling.PurchaseInput{Title: "Anschaffung", Body: "Erneuern", Category: "Allgemein", ToolID: tool.ID})
				if err != nil {
					t.Fatal(err)
				}
				entry = purchase.Entry
				path = fmt.Sprintf("/purchases/%d", entry.ID)
			}
			b := newBrowser(t, f.handler)
			b.login(f.other)
			form := url.Values{"side": {"vorgesetzter"}, "value": {"5"}, "version": {toolingID(entry.Version)}}
			if w := b.request("POST", path+"/priority", form, true, ""); w.Code != 403 {
				t.Fatalf("employee sets supervisor priority: %d %s", w.Code, w.Body)
			}
			form.Set("side", "mitarbeiter")
			form.Del("csrf")
			if w := b.request("POST", path+"/priority", form, false, ""); w.Code != 403 {
				t.Fatalf("priority without csrf: %d", w.Code)
			}
			if w := b.request("POST", path+"/priority", form, true, ""); w.Code != 303 {
				t.Fatalf("employee priority: %d %s", w.Code, w.Body)
			}
			if w := b.request("POST", path+"/priority", form, true, ""); w.Code != 409 {
				t.Fatalf("stale priority: %d %s", w.Code, w.Body)
			}
			manager := newBrowser(t, f.handler)
			manager.login(f.supervisor)
			form.Set("side", "vorgesetzter")
			form.Set("value", "1")
			form.Set("version", toolingID(entry.Version+1))
			if w := manager.request("POST", path+"/priority", form, true, ""); w.Code != 303 {
				t.Fatalf("supervisor priority: %d %s", w.Code, w.Body)
			}
			if w := b.request("GET", path, nil, false, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Prioritäten weichen stark ab") {
				t.Fatalf("priority divergence: %d %s", w.Code, w.Body)
			}
			if w := b.request("POST", path+"/comments", url.Values{"body": {"Kommentar zum Werkzeug"}}, true, ""); w.Code != 303 {
				t.Fatalf("tooling comment: %d %s", w.Code, w.Body)
			}
			comments, err := f.store.Comments(ctx, f.employee.ID, entry.ID)
			if err != nil || len(comments) != 1 || comments[0].Signature.Name != f.other.Name {
				t.Fatalf("tooling comment signature: %#v %v", comments, err)
			}
			got, err := f.store.Entry(ctx, f.employee.ID, entry.ID)
			if err != nil || got.Signature != entry.Signature || got.EmployeePriority == nil || *got.EmployeePriority != 5 || got.SupervisorPriority == nil || *got.SupervisorPriority != 1 {
				t.Fatalf("priority damaged state: %#v %v", got, err)
			}
		})
	}
}

func TestPurchaseHTTPBoardCombinesFiltersAndOrdering(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	makeWish := func(title, category string, employee, supervisor *int) tooling.Purchase {
		p, err := f.store.CreatePurchase(ctx, f.employee.ID, tooling.PurchaseInput{Title: title, Body: "Beschreibung", Category: category})
		if err != nil {
			t.Fatal(err)
		}
		for _, side := range []struct {
			actor int64
			side  core.PrioritySide
			value *int
		}{{f.employee.ID, core.PriorityEmployee, employee}, {f.supervisor.ID, core.PrioritySupervisor, supervisor}} {
			if side.value == nil {
				continue
			}
			if err := f.store.SetPriority(ctx, side.actor, p.ID, core.PriorityChange{Side: side.side, Value: side.value, Version: p.Version}); err != nil {
				t.Fatal(err)
			}
			p.Version++
		}
		return p
	}
	pointer := func(v int) *int { return &v }
	a := makeWish("Wunsch Alpha", "Handwerkzeug", pointer(5), pointer(1))
	b := makeWish("Wunsch Beta", "Diagnose", pointer(2), pointer(5))
	makeWish("Wunsch Gamma", "Handwerkzeug", nil, nil)
	if err := f.store.UpdatePurchase(ctx, f.supervisor.ID, b.ID, tooling.PurchaseUpdate{Title: b.Title, Body: b.Body, Category: b.Category, Location: b.Location, Status: "bestellt", Version: b.Version}); err != nil {
		t.Fatal(err)
	}
	viewer := newBrowser(t, f.handler)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"?sort=mitarbeiter", []string{a.Title, b.Title, "Wunsch Gamma"}},
		{"?sort=vorgesetzter", []string{b.Title, a.Title, "Wunsch Gamma"}},
		{"?category=Handwerkzeug&employee_priority=5&supervisor_priority=1", []string{a.Title}},
		{"?employee_priority=unbewertet", []string{"Wunsch Gamma"}},
		{"?status=bestellt", []string{b.Title}},
		{"?divergent=1", []string{b.Title, a.Title}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			w := viewer.request("GET", "/purchases"+tc.query, nil, false, "")
			if w.Code != 200 {
				t.Fatalf("filtered purchase board: %d %s", w.Code, w.Body)
			}
			body := w.Body.String()
			position := -1
			for _, title := range tc.want {
				index := strings.Index(body, ">"+title+"</a>")
				if index < 0 || index <= position {
					t.Fatalf("missing/out-of-order %q: %s", title, body)
				}
				position = index
			}
			for _, title := range []string{a.Title, b.Title, "Wunsch Gamma"} {
				wanted := false
				for _, v := range tc.want {
					wanted = wanted || v == title
				}
				if !wanted && strings.Contains(body, ">"+title+"</a>") {
					t.Fatalf("filtered-out %q present", title)
				}
			}
		})
	}
	for _, query := range []string{"?status=neu", "?sort=unsafe", "?employee_priority=6", "?supervisor_priority=abc"} {
		if w := viewer.request("GET", "/purchases"+query, nil, false, ""); w.Code != 400 {
			t.Fatalf("invalid purchase filter %q: %d", query, w.Code)
		}
	}
}

func TestToolReportPrefilledTitleAcceptsMaximumUnicodeToolName(t *testing.T) {
	f := setup(t)
	tool := toolingFixtureTool(t, f, strings.Repeat("Ä&", 100))
	anonymous := newBrowser(t, f.handler)
	path := fmt.Sprintf("/tools/%d/reports/new", tool.ID)
	w := anonymous.request("GET", path, nil, false, "")
	if w.Code != 200 {
		t.Fatalf("report form for long tool name: %d %s", w.Code, w.Body)
	}
	input := regexp.MustCompile(`<input\b[^>]*\bid="title"[^>]*\bvalue="([^"]*)"`).FindStringSubmatch(w.Body.String())
	if len(input) != 2 {
		t.Fatalf("report title input missing: %s", w.Body)
	}
	title := html.UnescapeString(input[1])
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) == 0 || utf8.RuneCountInString(title) > 200 {
		t.Fatalf("prefilled title is not a valid bounded Unicode title: %q", title)
	}
	form := url.Values{"author_id": {toolingID(f.employee.ID)}, "title": {title}, "body": {"Zustand des Werkzeugs prüfen"}, "observed_condition": {"defekt"}}
	if result := anonymous.request("POST", path, form, true, ""); result.Code != 303 {
		t.Fatalf("unchanged prefilled title cannot be submitted: %d %s", result.Code, result.Body)
	}
	reports, err := f.store.ToolReports(context.Background(), tooling.ReportFilter{ToolID: tool.ID})
	if err != nil || len(reports) != 1 || reports[0].Title != title {
		t.Fatalf("prefilled report title changed or failed: %#v %v", reports, err)
	}
}
