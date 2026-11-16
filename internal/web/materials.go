package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

type materialCard struct {
	materials.Material
	Low bool
}

type materialHistoryFilter struct {
	Employee, Supervisor, Sort string
}

func (a *app) materialBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := materials.Filter{Activity: q.Get("activity"), Status: q.Get("status"), Search: q.Get("search")}
	items, err := a.store.Materials(r.Context(), filter)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	cards := make([]materialCard, 0, len(items))
	for _, item := range items {
		cards = append(cards, materialCard{Material: item, Low: materials.IsLow(item.Stock, item.Minimum)})
	}
	a.render(w, r, "materials", page{Title: "Verbrauchsmaterialien", Current: &u, Materials: cards, MaterialFilter: filter})
}

func (a *app) newMaterial(w http.ResponseWriter, r *http.Request, u core.User) {
	if !materials.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	a.render(w, r, "material-form", page{Title: "Neues Verbrauchsmaterial", Current: &u, Material: materialCard{Material: materials.Material{Active: true}}})
}

func (a *app) createMaterial(w http.ResponseWriter, r *http.Request, u core.User) {
	if !materials.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	stock, err := materials.ParseQuantity(r.PostForm.Get("stock"))
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	minimum, err := materials.ParseQuantity(r.PostForm.Get("minimum"))
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	item, err := a.store.CreateMaterial(r.Context(), u.ID, materials.Input{Name: r.PostForm.Get("name"), Unit: r.PostForm.Get("unit"), Location: r.PostForm.Get("location"), Stock: stock, Minimum: minimum})
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/materials/%d", item.ID), http.StatusSeeOther)
}

func (a *app) editMaterial(w http.ResponseWriter, r *http.Request, u core.User) {
	if !materials.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	item, err := a.store.Material(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	a.render(w, r, "material-form", page{Title: "Verbrauchsmaterial bearbeiten", Current: &u, Material: materialCard{Material: item, Low: materials.IsLow(item.Stock, item.Minimum)}, Editing: true})
}

func (a *app) updateMaterial(w http.ResponseWriter, r *http.Request, u core.User) {
	if !materials.CanManage(u) {
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
		a.failMaterials(w, err)
		return
	}
	minimum, err := materials.ParseQuantity(r.PostForm.Get("minimum"))
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	_, err = a.store.UpdateMaterial(r.Context(), u.ID, id, materials.Update{Name: r.PostForm.Get("name"), Location: r.PostForm.Get("location"), Minimum: minimum, Active: r.PostForm.Get("active") == "1", Version: version})
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/materials/%d", id), http.StatusSeeOther)
}

func (a *app) filteredMaterialChecks(r *http.Request, checks []materials.Check) ([]materials.Check, materialHistoryFilter, error) {
	q := r.URL.Query()
	filter := materialHistoryFilter{Employee: q.Get("employee_priority"), Supervisor: q.Get("supervisor_priority"), Sort: q.Get("sort")}
	if filter.Sort != "" && filter.Sort != "neueste" && filter.Sort != "mitarbeiter" && filter.Sort != "vorgesetzter" {
		return nil, filter, core.ErrValidation
	}
	employee, employeeUnset, err := parsePriorityFilter(filter.Employee)
	if err != nil {
		return nil, filter, err
	}
	supervisor, supervisorUnset, err := parsePriorityFilter(filter.Supervisor)
	if err != nil {
		return nil, filter, err
	}
	if employee != nil {
		filter.Employee = strconv.Itoa(*employee)
	}
	if supervisor != nil {
		filter.Supervisor = strconv.Itoa(*supervisor)
	}
	scale := a.store.PriorityScale()
	for _, value := range []*int{employee, supervisor} {
		if value != nil && (*value < scale.Min || *value > scale.Max) {
			return nil, filter, core.ErrValidation
		}
	}
	result := make([]materials.Check, 0, len(checks))
	for _, check := range checks {
		if employeeUnset && check.EmployeePriority != nil || supervisorUnset && check.SupervisorPriority != nil {
			continue
		}
		if employee != nil && (check.EmployeePriority == nil || *check.EmployeePriority != *employee) || supervisor != nil && (check.SupervisorPriority == nil || *check.SupervisorPriority != *supervisor) {
			continue
		}
		result = append(result, check)
	}
	sort.SliceStable(result, func(i, j int) bool {
		var first, second *int
		if filter.Sort == "mitarbeiter" {
			first, second = result[i].EmployeePriority, result[j].EmployeePriority
		} else if filter.Sort == "vorgesetzter" {
			first, second = result[i].SupervisorPriority, result[j].SupervisorPriority
		}
		if first == nil && second != nil {
			return false
		}
		if first != nil && second == nil {
			return true
		}
		if first != nil && second != nil && *first != *second {
			return *first > *second
		}
		return result[i].ID > result[j].ID
	})
	return result, filter, nil
}

func (a *app) material(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	item, err := a.store.Material(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	checks, err := a.store.MaterialChecks(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	checks, filter, err := a.filteredMaterialChecks(r, checks)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	alerts, err := a.store.MaterialAlerts(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	authors, err := a.store.ActiveAuthors(r.Context())
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	a.render(w, r, "material", page{Title: item.Name, Current: &u, Material: materialCard{Material: item, Low: materials.IsLow(item.Stock, item.Minimum)}, MaterialChecks: checks, MaterialAlerts: alerts, MaterialHistoryFilter: filter, Authors: authors, CanManage: materials.CanManage(u), CanApprove: materials.CanAcknowledge(u)})
}

func (a *app) recordMaterialCheck(w http.ResponseWriter, r *http.Request, _ core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	author, err := a.selectedAuthor(r)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	quantity, err := materials.ParseQuantity(r.PostForm.Get("quantity"))
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	check, err := a.store.RecordMaterialCheck(r.Context(), author.ID, id, materials.CheckInput{Quantity: quantity, Note: r.PostForm.Get("note"), Version: version})
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/material-checks/%d", check.ID), http.StatusSeeOther)
}

func (a *app) materialCheck(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	check, err := a.store.MaterialCheck(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	comments, err := a.store.PublicComments(r.Context(), id)
	if err != nil {
		a.failMaterials(w, err)
		return
	}
	a.render(w, r, "material-check", page{Title: check.Title, Current: &u, MaterialCheck: check, Entry: check.Entry, MaterialCheckMode: true, Comments: comments})
}

func (a *app) materialCheckComment(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if _, err = a.store.MaterialCheck(r.Context(), id); err != nil {
		a.failMaterials(w, err)
		return
	}
	if err = a.store.AddComment(r.Context(), u.ID, id, r.PostForm.Get("body")); err != nil {
		a.failMaterials(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/material-checks/%d", id), http.StatusSeeOther)
}

func (a *app) acknowledgeMaterialAlert(w http.ResponseWriter, r *http.Request, u core.User) {
	if !materials.CanAcknowledge(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	alertID, err := positiveID(r.PathValue("alertID"), false)
	if err != nil {
		a.fail(w, core.ErrNotFound)
		return
	}
	if _, err = a.store.AcknowledgeMaterialAlert(r.Context(), u.ID, id, alertID); err != nil {
		a.failMaterials(w, err)
		return
	}
	target := fmt.Sprintf("/materials/%d", id)
	if r.PostForm.Get("return_to") == "dashboard" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *app) failMaterials(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrValidation) {
		http.Error(w, "Bitte gültige Materialangaben verwenden. Name und Einheit sind Pflicht. Mengen müssen zwischen 0 und 999999999,999 liegen und dürfen höchstens drei Nachkommastellen haben; Komma oder Punkt sind erlaubt. Texte und Prioritäten müssen innerhalb der angezeigten Grenzen liegen.", http.StatusBadRequest)
		return
	}
	a.fail(w, err)
}
