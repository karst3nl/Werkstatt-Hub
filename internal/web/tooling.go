package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tooling"
)

func conditionLabel(value string) string {
	switch value {
	case "ok":
		return "OK"
	case "verschleisst":
		return "Verschleißt"
	case "defekt":
		return "Defekt"
	case "ersetzt":
		return "Ersetzt"
	}
	return value
}
func reportTitle(name string) string {
	title := []rune("Zustandsmeldung: " + name)
	if len(title) > 200 {
		title = title[:200]
	}
	return string(title)
}
func positiveID(value string, optional bool) (int64, error) {
	if optional && (value == "" || value == "0") {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, core.ErrValidation
	}
	return id, nil
}
func formVersion(r *http.Request) (int64, error) {
	v, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil || v < 1 {
		return 0, core.ErrValidation
	}
	return v, nil
}
func (a *app) toolBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := tooling.ToolFilter{Category: q.Get("category"), Condition: q.Get("condition"), Search: q.Get("search")}
	list, err := a.store.Tools(r.Context(), filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.ToolCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tools", page{Title: "Werkzeuge", Current: &u, Tools: list, ToolFilter: filter, Categories: categories})
}
func (a *app) newTool(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	authors, err := a.managerAuthors(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.ToolCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tool-form", page{Title: "Neues Werkzeug", Current: &u, Authors: authors, Categories: categories, Tool: tooling.Tool{Condition: "ok", Category: "Allgemein"}})
}
func (a *app) createTool(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	author, err := a.selectedAuthor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !core.CanManage(author) {
		a.fail(w, core.ErrValidation)
		return
	}
	tool, err := a.store.CreateTool(r.Context(), u.ID, tooling.ToolInput{Name: r.PostForm.Get("name"), Category: r.PostForm.Get("category"), Location: r.PostForm.Get("location"), Condition: r.PostForm.Get("condition"), AuthorID: author.ID})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tools/%d", tool.ID), http.StatusSeeOther)
}
func (a *app) editTool(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	tool, err := a.store.Tool(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.ToolCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tool-form", page{Title: "Werkzeug bearbeiten", Current: &u, Tool: tool, Editing: true, Categories: categories})
}
func (a *app) updateTool(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	err = a.store.UpdateTool(r.Context(), u.ID, id, tooling.ToolUpdate{Name: r.PostForm.Get("name"), Category: r.PostForm.Get("category"), Location: r.PostForm.Get("location"), Condition: r.PostForm.Get("condition"), Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tools/%d", id), http.StatusSeeOther)
}
func parseToolingPriorities(r *http.Request, prefix string) (*int, bool, *int, bool, bool, error) {
	q := r.URL.Query()
	employee, employeeUnset, err := parsePriorityFilter(q.Get(prefix + "employee_priority"))
	if err != nil {
		return nil, false, nil, false, false, err
	}
	supervisor, supervisorUnset, err := parsePriorityFilter(q.Get(prefix + "supervisor_priority"))
	if err != nil {
		return nil, false, nil, false, false, err
	}
	raw := q.Get(prefix + "divergent")
	if raw != "" && raw != "1" {
		return nil, false, nil, false, false, core.ErrValidation
	}
	return employee, employeeUnset, supervisor, supervisorUnset, raw == "1", nil
}
func (a *app) tool(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	tool, err := a.store.Tool(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	q := r.URL.Query()
	filter := tooling.ReportFilter{ToolID: id, Status: q.Get("report_status"), Sort: q.Get("report_sort")}
	filter.EmployeePriority, filter.EmployeeUnset, filter.SupervisorPriority, filter.SupervisorUnset, filter.OnlyDivergent, err = parseToolingPriorities(r, "report_")
	if err != nil {
		a.fail(w, err)
		return
	}
	reports, err := a.store.ToolReports(r.Context(), filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	purchases, err := a.store.Purchases(r.Context(), tooling.PurchaseFilter{ToolID: id})
	if err != nil {
		a.fail(w, err)
		return
	}
	history, err := a.store.ToolHistory(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tool", page{Title: tool.Name, Current: &u, Tool: tool, Reports: reports, Purchases: purchases, Events: history, ReportFilter: filter, PriorityFilterPrefix: "report_", FilterEmployee: q.Get("report_employee_priority"), FilterSupervisor: q.Get("report_supervisor_priority"), SortFilter: filter.Sort, Divergent: filter.OnlyDivergent})
}
func (a *app) newToolReport(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	tool, err := a.store.Tool(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	authors, err := a.store.ActiveAuthors(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tooling-form", page{Title: "Zustand melden", Current: &u, Tool: tool, Authors: authors, ReportMode: true, Report: tooling.Report{ToolID: tool.ID, ToolName: tool.Name}, Entry: core.Entry{Title: reportTitle(tool.Name)}})
}
func (a *app) createToolReport(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	author, err := a.selectedAuthor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	report, err := a.store.CreateToolReport(r.Context(), author.ID, tooling.ReportInput{ToolID: id, Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), ObservedCondition: r.PostForm.Get("observed_condition")})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tool-reports/%d", report.ID), http.StatusSeeOther)
}
func (a *app) toolReport(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	report, err := a.store.ToolReport(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	comments, err := a.store.PublicComments(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "entry", page{Title: report.Title, Current: &u, Entry: report.Entry, Report: report, ReportMode: true, Comments: comments, CanEdit: core.CanEditEntry(u, report.Entry), CanManage: core.CanManage(u)})
}
func (a *app) editToolReport(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	report, err := a.store.ToolReport(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !core.CanEditEntry(u, report.Entry) {
		a.fail(w, core.ErrForbidden)
		return
	}
	a.render(w, r, "tooling-form", page{Title: "Werkzeugmeldung bearbeiten", Current: &u, Entry: report.Entry, Report: report, ReportMode: true, Editing: true, CanManage: core.CanManage(u)})
}
func (a *app) updateToolReport(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	status := r.PostForm.Get("status")
	if !core.CanManage(u) && status == "" {
		report, err := a.store.ToolReport(r.Context(), id)
		if err != nil {
			a.fail(w, err)
			return
		}
		status = report.Status
	}
	err = a.store.UpdateToolReport(r.Context(), u.ID, id, tooling.ReportUpdate{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), ObservedCondition: r.PostForm.Get("observed_condition"), Status: status, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tool-reports/%d", id), http.StatusSeeOther)
}
func (a *app) purchaseBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := tooling.PurchaseFilter{Category: q.Get("category"), Status: q.Get("status"), Sort: q.Get("sort")}
	var err error
	filter.ToolID, err = positiveID(q.Get("tool_id"), true)
	if err != nil {
		a.fail(w, err)
		return
	}
	filter.EmployeePriority, filter.EmployeeUnset, filter.SupervisorPriority, filter.SupervisorUnset, filter.OnlyDivergent, err = parseToolingPriorities(r, "")
	if err != nil {
		a.fail(w, err)
		return
	}
	list, err := a.store.Purchases(r.Context(), filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.PurchaseCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	tools, err := a.store.Tools(r.Context(), tooling.ToolFilter{})
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "purchases", page{Title: "Anschaffungswünsche", Current: &u, Purchases: list, PurchaseFilter: filter, Tools: tools, Categories: categories, FilterEmployee: q.Get("employee_priority"), FilterSupervisor: q.Get("supervisor_priority"), SortFilter: filter.Sort, Divergent: filter.OnlyDivergent})
}
func (a *app) newPurchase(w http.ResponseWriter, r *http.Request, u core.User) {
	authors, err := a.store.ActiveAuthors(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.PurchaseCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	tools, err := a.store.Tools(r.Context(), tooling.ToolFilter{})
	if err != nil {
		a.fail(w, err)
		return
	}
	id, err := positiveID(r.URL.Query().Get("tool_id"), true)
	if err != nil {
		a.fail(w, err)
		return
	}
	if id > 0 {
		if _, err = a.store.Tool(r.Context(), id); err != nil {
			a.fail(w, err)
			return
		}
	}
	a.render(w, r, "tooling-form", page{Title: "Neuer Anschaffungswunsch", Current: &u, Authors: authors, Categories: categories, Tools: tools, Purchase: tooling.Purchase{ToolID: id, Category: "Allgemein"}, PurchaseMode: true})
}
func (a *app) createPurchase(w http.ResponseWriter, r *http.Request, u core.User) {
	author, err := a.selectedAuthor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	id, err := positiveID(r.PostForm.Get("tool_id"), true)
	if err != nil {
		a.fail(w, err)
		return
	}
	purchase, err := a.store.CreatePurchase(r.Context(), author.ID, tooling.PurchaseInput{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Category: r.PostForm.Get("category"), Location: r.PostForm.Get("location"), ToolID: id})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/purchases/%d", purchase.ID), http.StatusSeeOther)
}
func (a *app) purchase(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	purchase, err := a.store.Purchase(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	comments, err := a.store.PublicComments(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "entry", page{Title: purchase.Title, Current: &u, Entry: purchase.Entry, Purchase: purchase, PurchaseMode: true, Comments: comments, CanEdit: tooling.CanEditPurchase(u, purchase), CanManage: core.CanManage(u)})
}
func (a *app) editPurchase(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	purchase, err := a.store.Purchase(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !tooling.CanEditPurchase(u, purchase) {
		a.fail(w, core.ErrForbidden)
		return
	}
	categories, err := a.store.PurchaseCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "tooling-form", page{Title: "Anschaffungswunsch bearbeiten", Current: &u, Entry: purchase.Entry, Purchase: purchase, PurchaseMode: true, Editing: true, Categories: categories, CanManage: core.CanManage(u)})
}
func (a *app) updatePurchase(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	status := r.PostForm.Get("status")
	if !core.CanManage(u) && status == "" {
		purchase, err := a.store.Purchase(r.Context(), id)
		if err != nil {
			a.fail(w, err)
			return
		}
		status = purchase.Status
	}
	err = a.store.UpdatePurchase(r.Context(), u.ID, id, tooling.PurchaseUpdate{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Category: r.PostForm.Get("category"), Location: r.PostForm.Get("location"), Status: status, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/purchases/%d", id), http.StatusSeeOther)
}
func (a *app) toolingComment(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	base := "/purchases"
	if strings.HasPrefix(r.URL.Path, "/tool-reports/") {
		base = "/tool-reports"
		_, err = a.store.ToolReport(r.Context(), id)
	} else {
		_, err = a.store.Purchase(r.Context(), id)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	if err = a.store.AddComment(r.Context(), u.ID, id, r.PostForm.Get("body")); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/%d", base, id), http.StatusSeeOther)
}
