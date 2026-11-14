package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

func priorityValue(value *int) string {
	if value == nil {
		return "Noch nicht gesetzt"
	}
	return strconv.Itoa(*value)
}
func prioritySelected(value *int, option int) bool { return value != nil && *value == option }

func parsePriorityFilter(value string) (*int, bool, error) {
	if value == "" {
		return nil, false, nil
	}
	if value == "unbewertet" {
		return nil, true, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return nil, false, core.ErrValidation
	}
	return &n, false, nil
}

func (a *app) ideaBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := ideas.Filter{Category: q.Get("category"), Status: q.Get("status"), Sort: q.Get("sort")}
	var err error
	filter.EmployeePriority, filter.EmployeeUnset, err = parsePriorityFilter(q.Get("employee_priority"))
	if err != nil {
		a.fail(w, err)
		return
	}
	filter.SupervisorPriority, filter.SupervisorUnset, err = parsePriorityFilter(q.Get("supervisor_priority"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if v := q.Get("divergent"); v != "" {
		if v != "1" {
			a.fail(w, core.ErrValidation)
			return
		}
		filter.OnlyDivergent = true
	}
	list, err := a.store.Ideas(r.Context(), u.ID, filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	categories, err := a.store.IdeaCategories(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "ideas", page{Title: "Ideenboard", Current: &u, Ideas: list, Categories: categories, Filter: filter, FilterEmployee: q.Get("employee_priority"), FilterSupervisor: q.Get("supervisor_priority")})
}
func (a *app) newIdea(w http.ResponseWriter, r *http.Request, u core.User) {
	categories, err := a.store.IdeaCategories(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "form", page{Title: "Neue Idee", Current: &u, IdeaMode: true, Category: "Allgemein", Categories: categories})
}
func (a *app) createIdea(w http.ResponseWriter, r *http.Request, u core.User) {
	idea, err := a.store.CreateIdea(r.Context(), u.ID, ideas.Input{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Category: r.PostForm.Get("category")})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ideas/%d", idea.ID), http.StatusSeeOther)
}
func (a *app) idea(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	idea, err := a.store.Idea(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	comments, err := a.store.Comments(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "entry", page{Title: idea.Title, Current: &u, Entry: idea.Entry, Category: idea.Category, IdeaMode: true, Comments: comments, CanEdit: core.CanEditEntry(u, idea.Entry), CanManage: core.CanManage(u)})
}
func (a *app) editIdea(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	idea, err := a.store.Idea(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if !core.CanEditEntry(u, idea.Entry) {
		a.fail(w, core.ErrForbidden)
		return
	}
	categories, err := a.store.IdeaCategories(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "form", page{Title: "Idee bearbeiten", Current: &u, Entry: idea.Entry, Category: idea.Category, Categories: categories, IdeaMode: true, Editing: true, CanManage: core.CanManage(u)})
}
func (a *app) updateIdea(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil {
		a.fail(w, core.ErrValidation)
		return
	}
	status := r.PostForm.Get("status")
	if !core.CanManage(u) && status == "" {
		idea, err := a.store.Idea(r.Context(), u.ID, id)
		if err != nil {
			a.fail(w, err)
			return
		}
		status = idea.Status
	}
	err = a.store.UpdateIdea(r.Context(), u.ID, id, ideas.Update{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Category: r.PostForm.Get("category"), Status: status, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ideas/%d", id), http.StatusSeeOther)
}
func (a *app) ideaComment(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if _, err = a.store.Idea(r.Context(), u.ID, id); err != nil {
		a.fail(w, err)
		return
	}
	if err = a.store.AddComment(r.Context(), u.ID, id, r.PostForm.Get("body")); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ideas/%d", id), http.StatusSeeOther)
}
func (a *app) setPriority(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	entry, err := a.store.Entry(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	ideaRoute := strings.HasPrefix(r.URL.Path, "/ideas/")
	if ideaRoute && entry.Module != "idee" {
		a.fail(w, core.ErrNotFound)
		return
	}
	// Reject a mismatched mutation route instead of redirecting a POST.
	if !ideaRoute && entry.Module != "basis" {
		a.fail(w, core.ErrValidation)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil {
		a.fail(w, core.ErrValidation)
		return
	}
	var value *int
	if raw := r.PostForm.Get("value"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			a.fail(w, core.ErrValidation)
			return
		}
		value = &n
	}
	err = a.store.SetPriority(r.Context(), u.ID, id, core.PriorityChange{Side: core.PrioritySide(r.PostForm.Get("side")), Value: value, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	base := "/entries"
	if ideaRoute {
		base = "/ideas"
	}
	http.Redirect(w, r, fmt.Sprintf("%s/%d", base, id), http.StatusSeeOther)
}
