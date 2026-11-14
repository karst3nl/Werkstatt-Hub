package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
)

type taskScanner struct {
	scanner
	fields []any
}

func (s taskScanner) Scan(dest ...any) error { return s.scanner.Scan(append(dest, s.fields...)...) }
func taskColumns() string {
	return "e." + strings.ReplaceAll(entryColumns, ",", ",e.") + ",t.assignee_id,u.name,t.due_date,t.approved_by,t.approved_name,t.approved_at"
}
func scanTask(row scanner) (tasks.Task, error) {
	var task tasks.Task
	var approvedBy sql.NullInt64
	var approvedName, approvedAt sql.NullString
	entry, err := scanEntry(taskScanner{row, []any{&task.AssigneeID, &task.AssigneeName, &task.DueDate, &approvedBy, &approvedName, &approvedAt}})
	task.Entry = entry
	if err != nil {
		return task, err
	}
	if approvedBy.Valid {
		at, err := parseTime(approvedAt.String)
		if err != nil {
			return task, err
		}
		task.Approval = &tasks.Approval{UserID: approvedBy.Int64, Signature: core.Signature{Name: approvedName.String, At: at}}
	}
	return task, nil
}

const taskJoin = " FROM entries e JOIN tasks t ON t.entry_id=e.id JOIN users u ON u.id=t.assignee_id WHERE e.module='aufgabe'"

func taskFrom(ctx context.Context, q queryer, id int64) (tasks.Task, error) {
	return scanTask(q.QueryRowContext(ctx, "SELECT "+taskColumns()+taskJoin+" AND e.id=?", id))
}
func (s *Store) Task(ctx context.Context, actorID, id int64) (tasks.Task, error) {
	var result tasks.Task
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		var err error
		result, err = taskFrom(ctx, tx, id)
		return err
	})
	return result, err
}
func (s *Store) PublicTask(ctx context.Context, id int64) (tasks.Task, error) {
	return taskFrom(ctx, s.db, id)
}
func taskQuery(filter tasks.Filter) (string, []any, error) {
	if filter.AssigneeID < 0 || filter.Status != "" && !tasks.ValidStatus(filter.Status) {
		return "", nil, core.ErrValidation
	}
	query := "SELECT " + taskColumns() + taskJoin
	args := []any{}
	if filter.AssigneeID > 0 {
		query += " AND t.assignee_id=?"
		args = append(args, filter.AssigneeID)
	}
	if filter.Status != "" {
		query += " AND e.status=?"
		args = append(args, filter.Status)
	}
	return query + " ORDER BY t.due_date,e.id DESC", args, nil
}
func taskList(ctx context.Context, q queryer, query string, args []any) ([]tasks.Task, error) {
	result := make([]tasks.Task, 0)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}
func (s *Store) Tasks(ctx context.Context, actorID int64, filter tasks.Filter) ([]tasks.Task, error) {
	query, args, err := taskQuery(filter)
	if err != nil {
		return nil, err
	}
	var result []tasks.Task
	err = s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		var err error
		result, err = taskList(ctx, tx, query, args)
		return err
	})
	return result, err
}
func (s *Store) PublicTasks(ctx context.Context, filter tasks.Filter) ([]tasks.Task, error) {
	query, args, err := taskQuery(filter)
	if err != nil {
		return nil, err
	}
	return taskList(ctx, s.db, query, args)
}
func (s *Store) TaskAssignees(ctx context.Context) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,role,active FROM users WHERE active=1 AND deleted=0 AND role=? ORDER BY name COLLATE NOCASE,id", core.RoleEmployee)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.User, 0)
	for rows.Next() {
		var user core.User
		if err = rows.Scan(&user.ID, &user.Name, &user.Role, &user.Active); err != nil {
			return nil, err
		}
		result = append(result, user)
	}
	return result, rows.Err()
}
func taskParticipant(ctx context.Context, q queryer, id int64, author bool) (core.User, error) {
	u, err := userFrom(ctx, q, id)
	if errors.Is(err, core.ErrNotFound) {
		return core.User{}, core.ErrValidation
	}
	if err != nil {
		return core.User{}, err
	}
	if !u.Active || u.Deleted || author && !core.CanManage(u) || !author && u.Role != core.RoleEmployee {
		return core.User{}, core.ErrValidation
	}
	return u, nil
}
func taskText(title, body, due string) (string, string, error) {
	title, err := cleanText(title, 200)
	if err != nil {
		return "", "", err
	}
	body, err = cleanText(body, 20000)
	if err != nil {
		return "", "", err
	}
	if !tasks.ValidDueDate(due) {
		return "", "", core.ErrValidation
	}
	return title, body, nil
}
func (s *Store) CreateTask(ctx context.Context, actorID int64, in tasks.Input) (tasks.Task, error) {
	title, body, err := taskText(in.Title, in.Body, in.DueDate)
	if err != nil {
		return tasks.Task{}, err
	}
	var result tasks.Task
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanManage(actor) {
			return core.ErrForbidden
		}
		author, err := taskParticipant(ctx, tx, in.AuthorID, true)
		if err != nil {
			return err
		}
		assignee, err := taskParticipant(ctx, tx, in.AssigneeID, false)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO entries(module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version)
   VALUES('aufgabe',?,?,?,'offen',?,?,?,?,1)`, author.ID, title, body, author.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tasks(entry_id,assignee_id,due_date) VALUES(?,?,?)", id, assignee.ID, in.DueDate); err != nil {
			return err
		}
		result = tasks.Task{Entry: core.Entry{ID: id, Module: "aufgabe", AuthorID: author.ID, Title: title, Body: body, Status: "offen", Signature: core.Signature{Name: author.Name, At: now}, CreatedAt: now, UpdatedAt: now, Version: 1}, AssigneeID: assignee.ID, AssigneeName: assignee.Name, DueDate: in.DueDate}
		return appendAudit(ctx, tx, actor, "task.create", "task", id, nil, result, now)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result, nil
}
func (s *Store) UpdateTask(ctx context.Context, actorID, id int64, in tasks.Update) error {
	title, body, err := taskText(in.Title, in.Body, in.DueDate)
	if err != nil {
		return err
	}
	if in.Version < 1 {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanManage(actor) {
			return core.ErrForbidden
		}
		before, err := taskFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Status == "erledigt" {
			return core.ErrValidation
		}
		assignee, err := taskParticipant(ctx, tx, in.AssigneeID, false)
		if err != nil {
			return err
		}
		if before.Title == title && before.Body == body && before.DueDate == in.DueDate && before.AssigneeID == assignee.ID {
			return nil
		}
		after := before
		after.Title = title
		after.Body = body
		after.DueDate = in.DueDate
		after.AssigneeID = assignee.ID
		after.AssigneeName = assignee.Name
		if after.AssigneeID != before.AssigneeID {
			after.Status = "offen"
			after.Approval = nil
		}
		after.UpdatedAt = time.Now().UTC()
		after.Version++
		r, err := tx.ExecContext(ctx, "UPDATE entries SET title=?,body=?,status=?,updated_at=?,version=? WHERE id=? AND version=?", title, body, after.Status, timestamp(after.UpdatedAt), after.Version, id, in.Version)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return core.ErrConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tasks SET assignee_id=?,due_date=? WHERE entry_id=?", after.AssigneeID, after.DueDate, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "task.update", "task", id, before, after, after.UpdatedAt)
	})
}
func (s *Store) ChangeTaskStatus(ctx context.Context, actorID, id int64, in tasks.StatusChange) error {
	if in.Version < 1 || !tasks.ValidStatus(in.Status) {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := taskFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if !tasks.CanChangeStatus(actor, before, in.Status) {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Status == in.Status {
			return nil
		}
		after := before
		after.Status = in.Status
		after.UpdatedAt = time.Now().UTC()
		after.Version++
		after.Approval = nil
		action := "task.status"
		var approvedBy, approvedName, approvedAt any
		if in.Status == "erledigt" {
			after.Approval = &tasks.Approval{UserID: actor.ID, Signature: core.Signature{Name: actor.Name, At: after.UpdatedAt}}
			approvedBy, approvedName, approvedAt = actor.ID, actor.Name, timestamp(after.UpdatedAt)
			action = "task.approve"
		}
		r, err := tx.ExecContext(ctx, "UPDATE entries SET status=?,updated_at=?,version=? WHERE id=? AND version=?", after.Status, timestamp(after.UpdatedAt), after.Version, id, in.Version)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return core.ErrConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tasks SET approved_by=?,approved_name=?,approved_at=? WHERE entry_id=?", approvedBy, approvedName, approvedAt, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, action, "task", id, before, after, after.UpdatedAt)
	})
}
