package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"werkstatt-hub/internal/core"
)

const entryColumns = `id,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version,employee_priority,supervisor_priority`

type scanner interface{ Scan(...any) error }

func scanEntry(row scanner) (core.Entry, error) {
	var e core.Entry
	var sign, created, updated string
	var employee, supervisor sql.NullInt64
	err := row.Scan(&e.ID, &e.AuthorID, &e.Title, &e.Body, &e.Status, &e.Signature.Name, &sign, &created, &updated, &e.Version, &employee, &supervisor)
	if errors.Is(err, sql.ErrNoRows) {
		return e, core.ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if e.Signature.At, err = parseTime(sign); err != nil {
		return e, err
	}
	if e.CreatedAt, err = parseTime(created); err != nil {
		return e, err
	}
	if e.UpdatedAt, err = parseTime(updated); err != nil {
		return e, err
	}
	if employee.Valid {
		v := int(employee.Int64)
		e.EmployeePriority = &v
	}
	if supervisor.Valid {
		v := int(supervisor.Int64)
		e.SupervisorPriority = &v
	}
	return e, nil
}
func entryFrom(ctx context.Context, q queryer, id int64) (core.Entry, error) {
	return scanEntry(q.QueryRowContext(ctx, "SELECT "+entryColumns+" FROM entries WHERE id=?", id))
}
func (s *Store) Entries(ctx context.Context, actorID int64) ([]core.Entry, error) {
	result := make([]core.Entry, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT "+entryColumns+" FROM entries ORDER BY id DESC")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEntry(rows)
			if err != nil {
				return err
			}
			result = append(result, e)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) Entry(ctx context.Context, actorID, id int64) (core.Entry, error) {
	var result core.Entry
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		var err error
		result, err = entryFrom(ctx, tx, id)
		return err
	})
	return result, err
}
func (s *Store) CreateEntry(ctx context.Context, actorID int64, in core.EntryInput) (core.Entry, error) {
	title, err := cleanText(in.Title, 200)
	if err != nil {
		return core.Entry{}, err
	}
	body, err := cleanText(in.Body, 20000)
	if err != nil {
		return core.Entry{}, err
	}
	var result core.Entry
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO entries(author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version)
   VALUES(?,?,?,'neu',?,?,?,?,1)`, actor.ID, title, body, actor.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result = core.Entry{ID: id, AuthorID: actor.ID, Title: title, Body: body, Status: "neu", Signature: core.Signature{Name: actor.Name, At: now}, CreatedAt: now, UpdatedAt: now, Version: 1}
		return appendAudit(ctx, tx, actor, "entry.create", "entry", id, nil, result, now)
	})
	if err != nil {
		return core.Entry{}, err
	}
	return result, nil
}
func validStatus(status string) bool {
	switch status {
	case "neu", "in_pruefung", "geplant", "erledigt", "abgelehnt":
		return true
	}
	return false
}
func (s *Store) UpdateEntry(ctx context.Context, actorID, id int64, in core.EntryUpdate) error {
	title, err := cleanText(in.Title, 200)
	if err != nil {
		return err
	}
	body, err := cleanText(in.Body, 20000)
	if err != nil {
		return err
	}
	if !validStatus(in.Status) || in.Version < 1 {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := entryFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if !core.CanEditEntry(actor, before) || (!core.CanManage(actor) && in.Status != before.Status) {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Title == title && before.Body == body && before.Status == in.Status {
			return nil
		}
		after := before
		after.Title = title
		after.Body = body
		after.Status = in.Status
		after.UpdatedAt = time.Now().UTC()
		after.Version++
		r, err := tx.ExecContext(ctx, "UPDATE entries SET title=?,body=?,status=?,updated_at=?,version=? WHERE id=? AND version=?", after.Title, after.Body, after.Status, timestamp(after.UpdatedAt), after.Version, id, in.Version)
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
		return appendAudit(ctx, tx, actor, "entry.update", "entry", id, before, after, after.UpdatedAt)
	})
}
func (s *Store) Comments(ctx context.Context, actorID, entryID int64) ([]core.Comment, error) {
	result := make([]core.Comment, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		if _, err := entryFrom(ctx, tx, entryID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,entry_id,author_id,body,signature_name,signature_at FROM comments WHERE entry_id=? ORDER BY id", entryID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c core.Comment
			var at string
			if err = rows.Scan(&c.ID, &c.EntryID, &c.AuthorID, &c.Body, &c.Signature.Name, &at); err != nil {
				return err
			}
			if c.Signature.At, err = parseTime(at); err != nil {
				return err
			}
			result = append(result, c)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) AddComment(ctx context.Context, actorID, entryID int64, body string) error {
	body, err := cleanText(body, 20000)
	if err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if _, err = entryFrom(ctx, tx, entryID); err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, "INSERT INTO comments(entry_id,author_id,body,signature_name,signature_at) VALUES(?,?,?,?,?)", entryID, actor.ID, body, actor.Name, timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		after := core.Comment{ID: id, EntryID: entryID, AuthorID: actor.ID, Body: body, Signature: core.Signature{Name: actor.Name, At: now}}
		return appendAudit(ctx, tx, actor, "comment.create", "comment", id, nil, after, now)
	})
}
