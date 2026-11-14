package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

// Reuse the common entry scanner while appending the module-specific category.
type ideaScanner struct {
	scanner
	category *string
}

func (s ideaScanner) Scan(dest ...any) error { return s.scanner.Scan(append(dest, s.category)...) }
func scanIdea(row scanner) (ideas.Idea, error) {
	var idea ideas.Idea
	entry, err := scanEntry(ideaScanner{row, &idea.Category})
	idea.Entry = entry
	return idea, err
}
func ideaColumns() string { return "e." + strings.ReplaceAll(entryColumns, ",", ",e.") + ",i.category" }
func ideaFrom(ctx context.Context, q queryer, id int64) (ideas.Idea, error) {
	return scanIdea(q.QueryRowContext(ctx, "SELECT "+ideaColumns()+" FROM entries e JOIN ideas i ON i.entry_id=e.id WHERE e.id=? AND e.module='idee'", id))
}
func (s *Store) Idea(ctx context.Context, actorID, id int64) (ideas.Idea, error) {
	var result ideas.Idea
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		var err error
		result, err = ideaFrom(ctx, tx, id)
		return err
	})
	return result, err
}
func ideaText(title, body, category string) (string, string, string, error) {
	title, err := cleanText(title, 200)
	if err != nil {
		return "", "", "", err
	}
	body, err = cleanText(body, 20000)
	if err != nil {
		return "", "", "", err
	}
	category, err = cleanName(category)
	return title, body, category, err
}
func (s *Store) CreateIdea(ctx context.Context, actorID int64, in ideas.Input) (ideas.Idea, error) {
	title, body, category, err := ideaText(in.Title, in.Body, in.Category)
	if err != nil {
		return ideas.Idea{}, err
	}
	var result ideas.Idea
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO entries(module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version)
   VALUES('idee',?,?,?,'neu',?,?,?,?,1)`, actor.ID, title, body, actor.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO ideas(entry_id,category) VALUES(?,?)", id, category); err != nil {
			return err
		}
		result = ideas.Idea{Entry: core.Entry{ID: id, Module: "idee", AuthorID: actor.ID, Title: title, Body: body, Status: "neu", Signature: core.Signature{Name: actor.Name, At: now}, CreatedAt: now, UpdatedAt: now, Version: 1}, Category: category}
		return appendAudit(ctx, tx, actor, "idea.create", "idea", id, nil, result, now)
	})
	if err != nil {
		return ideas.Idea{}, err
	}
	return result, nil
}
func (s *Store) UpdateIdea(ctx context.Context, actorID, id int64, in ideas.Update) error {
	title, body, category, err := ideaText(in.Title, in.Body, in.Category)
	if err != nil {
		return err
	}
	if !ideas.ValidStatus(in.Status) || in.Version < 1 {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := ideaFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if !core.CanEditEntry(actor, before.Entry) || (!core.CanManage(actor) && in.Status != before.Status) {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Title == title && before.Body == body && before.Category == category && before.Status == in.Status {
			return nil
		}
		after := before
		after.Title = title
		after.Body = body
		after.Category = category
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
		if _, err = tx.ExecContext(ctx, "UPDATE ideas SET category=? WHERE entry_id=?", category, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "idea.update", "idea", id, before, after, after.UpdatedAt)
	})
}
func (s *Store) validateIdeaFilter(f ideas.Filter) (ideas.Filter, error) {
	if f.Category != "" {
		var err error
		f.Category, err = cleanName(f.Category)
		if err != nil {
			return f, err
		}
	}
	if f.Status != "" && !ideas.ValidStatus(f.Status) {
		return f, core.ErrValidation
	}
	switch f.Sort {
	case "", "neueste", "mitarbeiter", "vorgesetzter":
	default:
		return f, core.ErrValidation
	}
	if f.EmployeeUnset && f.EmployeePriority != nil || f.SupervisorUnset && f.SupervisorPriority != nil {
		return f, core.ErrValidation
	}
	for _, value := range []*int{f.EmployeePriority, f.SupervisorPriority} {
		if value != nil && (*value < s.priorityScale.Min || *value > s.priorityScale.Max) {
			return f, core.ErrValidation
		}
	}
	return f, nil
}
func (s *Store) Ideas(ctx context.Context, actorID int64, filter ideas.Filter) ([]ideas.Idea, error) {
	f, err := s.validateIdeaFilter(filter)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + ideaColumns() + " FROM entries e JOIN ideas i ON i.entry_id=e.id WHERE e.module='idee'"
	args := []any{}
	if f.Category != "" {
		query += " AND i.category=?"
		args = append(args, f.Category)
	}
	if f.Status != "" {
		query += " AND e.status=?"
		args = append(args, f.Status)
	}
	if f.EmployeePriority != nil {
		query += " AND e.employee_priority=?"
		args = append(args, *f.EmployeePriority)
	}
	if f.SupervisorPriority != nil {
		query += " AND e.supervisor_priority=?"
		args = append(args, *f.SupervisorPriority)
	}
	if f.EmployeeUnset {
		query += " AND e.employee_priority IS NULL"
	}
	if f.SupervisorUnset {
		query += " AND e.supervisor_priority IS NULL"
	}
	if f.OnlyDivergent {
		query += " AND e.employee_priority IS NOT NULL AND e.supervisor_priority IS NOT NULL AND abs(e.employee_priority-e.supervisor_priority)>=?"
		args = append(args, (s.priorityScale.Max-s.priorityScale.Min)/2+1)
	}
	switch f.Sort {
	case "mitarbeiter":
		query += " ORDER BY e.employee_priority DESC NULLS LAST,e.id DESC"
	case "vorgesetzter":
		query += " ORDER BY e.supervisor_priority DESC NULLS LAST,e.id DESC"
	default:
		query += " ORDER BY e.id DESC"
	}
	result := make([]ideas.Idea, 0)
	err = s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			idea, err := scanIdea(rows)
			if err != nil {
				return err
			}
			result = append(result, idea)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) IdeaCategories(ctx context.Context, actorID int64) ([]string, error) {
	result := make([]string, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := actorFrom(ctx, tx, actorID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT i.category FROM ideas i JOIN entries e ON e.id=i.entry_id WHERE e.module='idee' ORDER BY i.category COLLATE NOCASE,i.category")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var category string
			if err = rows.Scan(&category); err != nil {
				return err
			}
			result = append(result, category)
		}
		return rows.Err()
	})
	return result, err
}
