package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
)

type taskCard struct {
	tasks.Task
	ReminderKind  string
	ReminderLabel string
}
type taskColumn struct {
	Status string
	Title  string
	Tasks  []taskCard
}
type taskStatusOption struct{ Value, Label string }

func taskDate(value string) string {
	d, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return d.Format("02.01.2006")
}
func reminderLabel(kind string) string {
	switch kind {
	case "ueberfaellig":
		return "Überfällig"
	case "heute":
		return "Heute fällig"
	case "bald":
		return "Bald fällig"
	default:
		return ""
	}
}
func (a *app) taskCard(t tasks.Task, now time.Time) taskCard {
	kind := a.store.TaskSettings().Reminder(t, now)
	return taskCard{Task: t, ReminderKind: kind, ReminderLabel: reminderLabel(kind)}
}
func (a *app) taskBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := tasks.Filter{Status: q.Get("status")}
	mine := q.Get("mine") != ""
	if mine && q.Get("mine") != "1" {
		a.fail(w, core.ErrValidation)
		return
	}
	if mine && !u.Active {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if mine {
		filter.AssigneeID = u.ID
	} else if raw := q.Get("assignee_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.fail(w, core.ErrValidation)
			return
		}
		filter.AssigneeID = id
	}
	list, err := a.store.PublicTasks(r.Context(), filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	assignees, err := a.store.TaskAssignees(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	columns := []taskColumn{{Status: "offen", Title: "Offen"}, {Status: "in_arbeit", Title: "In Arbeit"}, {Status: "zur_kontrolle", Title: "Zur Kontrolle"}, {Status: "erledigt", Title: "Erledigt"}}
	now := time.Now()
	for _, task := range list {
		for i := range columns {
			if columns[i].Status == task.Status {
				columns[i].Tasks = append(columns[i].Tasks, a.taskCard(task, now))
				break
			}
		}
	}
	title := "Aufgabenboard"
	if mine {
		title = "Meine Aufgaben"
	}
	a.render(w, r, "tasks", page{Title: title, Current: &u, TaskColumns: columns, TaskFilter: filter, Assignees: assignees, Mine: mine})
}
func (a *app) taskAuthors(r *http.Request) ([]core.User, error) {
	all, err := a.store.ActiveAuthors(r.Context())
	if err != nil {
		return nil, err
	}
	authors := make([]core.User, 0)
	for _, u := range all {
		if core.CanManage(u) {
			authors = append(authors, u)
		}
	}
	return authors, nil
}
func (a *app) newTask(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	assignees, err := a.store.TaskAssignees(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	authors, err := a.taskAuthors(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "task-form", page{Title: "Neue Aufgabe", Current: &u, TaskMode: true, Assignees: assignees, Authors: authors})
}
func (a *app) createTask(w http.ResponseWriter, r *http.Request, u core.User) {
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
	assignee, err := strconv.ParseInt(r.PostForm.Get("assignee_id"), 10, 64)
	if err != nil || assignee <= 0 {
		a.fail(w, core.ErrValidation)
		return
	}
	task, err := a.store.CreateTask(r.Context(), u.ID, tasks.Input{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), DueDate: r.PostForm.Get("due_date"), AssigneeID: assignee, AuthorID: author.ID})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", task.ID), http.StatusSeeOther)
}
func (a *app) task(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	task, err := a.store.PublicTask(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	comments, err := a.store.PublicComments(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	options := make([]taskStatusOption, 0)
	for _, target := range []string{"offen", "in_arbeit", "zur_kontrolle"} {
		if target != task.Status && tasks.CanChangeStatus(u, task, target) {
			options = append(options, taskStatusOption{Value: target, Label: statusLabel(target)})
		}
	}
	card := a.taskCard(task, time.Now())
	a.render(w, r, "entry", page{Title: task.Title, Current: &u, Entry: task.Entry, Task: task, TaskMode: true, Comments: comments, CanEdit: core.CanManage(u) && task.Status != "erledigt", CanManage: core.CanManage(u), TaskStatuses: options, CanApprove: task.Status == "zur_kontrolle" && tasks.CanChangeStatus(u, task, "erledigt"), TaskReminder: card})
}
func (a *app) editTask(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	task, err := a.store.Task(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if task.Status == "erledigt" {
		a.fail(w, core.ErrValidation)
		return
	}
	assignees, err := a.store.TaskAssignees(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	missing := true
	for _, user := range assignees {
		if user.ID == task.AssigneeID {
			missing = false
			break
		}
	}
	a.render(w, r, "task-form", page{Title: "Aufgabe bearbeiten", Current: &u, Entry: task.Entry, Task: task, TaskMode: true, Editing: true, Assignees: assignees, AssigneeMissing: missing})
}
func (a *app) updateTask(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanManage(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
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
	assignee, err := strconv.ParseInt(r.PostForm.Get("assignee_id"), 10, 64)
	if err != nil || assignee <= 0 {
		a.fail(w, core.ErrValidation)
		return
	}
	err = a.store.UpdateTask(r.Context(), u.ID, id, tasks.Update{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), DueDate: r.PostForm.Get("due_date"), AssigneeID: assignee, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", id), http.StatusSeeOther)
}
func (a *app) taskStatus(w http.ResponseWriter, r *http.Request, u core.User) {
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
	err = a.store.ChangeTaskStatus(r.Context(), u.ID, id, tasks.StatusChange{Status: r.PostForm.Get("status"), Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", id), http.StatusSeeOther)
}
func (a *app) taskComment(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if _, err = a.store.Task(r.Context(), u.ID, id); err != nil {
		a.fail(w, err)
		return
	}
	if err = a.store.AddComment(r.Context(), u.ID, id, r.PostForm.Get("body")); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", id), http.StatusSeeOther)
}
