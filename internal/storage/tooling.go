package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tooling"
)

const toolColumns = `t.id,t.author_id,t.name,t.category,t.location,t.condition,t.signature_name,t.signature_at,t.created_at,t.updated_at,t.version,
 (SELECT count(*) FROM tool_reports r JOIN entries e ON e.id=r.entry_id WHERE r.tool_id=t.id AND e.module='werkzeugmeldung' AND e.status IN ('neu','in_pruefung'))`

func scanTool(row scanner) (tooling.Tool, error) {
	var t tooling.Tool
	var signature, created, updated string
	err := row.Scan(&t.ID, &t.AuthorID, &t.Name, &t.Category, &t.Location, &t.Condition, &t.Signature.Name, &signature, &created, &updated, &t.Version, &t.OpenReports)
	if errors.Is(err, sql.ErrNoRows) {
		return t, core.ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if t.Signature.At, err = parseTime(signature); err != nil {
		return t, err
	}
	if t.CreatedAt, err = parseTime(created); err != nil {
		return t, err
	}
	if t.UpdatedAt, err = parseTime(updated); err != nil {
		return t, err
	}
	return t, nil
}
func toolFrom(ctx context.Context, q queryer, id int64) (tooling.Tool, error) {
	return scanTool(q.QueryRowContext(ctx, "SELECT "+toolColumns+" FROM tools t WHERE t.id=?", id))
}
func (s *Store) Tool(ctx context.Context, id int64) (tooling.Tool, error) {
	return toolFrom(ctx, s.db, id)
}
func optionalToolText(value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || strings.ContainsRune(value, 0) {
		return "", core.ErrValidation
	}
	return value, nil
}
func optionalCategory(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return cleanName(value)
}
func (s *Store) Tools(ctx context.Context, f tooling.ToolFilter) ([]tooling.Tool, error) {
	var err error
	f.Category, err = optionalCategory(f.Category)
	if err != nil {
		return nil, err
	}
	f.Search, err = optionalToolText(f.Search, 200)
	if err != nil {
		return nil, err
	}
	if f.Condition != "" && !tooling.ValidCondition(f.Condition) {
		return nil, core.ErrValidation
	}
	query := "SELECT " + toolColumns + " FROM tools t WHERE 1=1"
	args := []any{}
	if f.Category != "" {
		query += " AND t.category=?"
		args = append(args, f.Category)
	}
	if f.Condition != "" {
		query += " AND t.condition=?"
		args = append(args, f.Condition)
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY t.name COLLATE NOCASE,t.id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]tooling.Tool, 0)
	search := strings.ToLower(f.Search)
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		if search == "" || strings.Contains(strings.ToLower(t.Name), search) {
			result = append(result, t)
		}
	}
	return result, rows.Err()
}
func (s *Store) ToolCategories(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT category FROM tools ORDER BY category COLLATE NOCASE,category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func toolText(name, category, location, condition string) (string, string, string, error) {
	name, err := cleanText(name, 200)
	if err != nil {
		return "", "", "", err
	}
	category, err = cleanName(category)
	if err != nil {
		return "", "", "", err
	}
	location, err = cleanName(location)
	if err != nil {
		return "", "", "", err
	}
	if !tooling.ValidCondition(condition) {
		return "", "", "", core.ErrValidation
	}
	return name, category, location, nil
}
func selectedToolAuthor(ctx context.Context, q queryer, id int64) (core.User, error) {
	u, err := userFrom(ctx, q, id)
	if errors.Is(err, core.ErrNotFound) {
		return core.User{}, core.ErrValidation
	}
	if err != nil {
		return core.User{}, err
	}
	if !core.CanManage(u) {
		return core.User{}, core.ErrValidation
	}
	return u, nil
}
func (s *Store) CreateTool(ctx context.Context, actorID int64, in tooling.ToolInput) (tooling.Tool, error) {
	name, category, location, err := toolText(in.Name, in.Category, in.Location, in.Condition)
	if err != nil {
		return tooling.Tool{}, err
	}
	var result tooling.Tool
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanManage(actor) {
			return core.ErrForbidden
		}
		author, err := selectedToolAuthor(ctx, tx, in.AuthorID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO tools(author_id,name,category,location,condition,signature_name,signature_at,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,?,?,1)`, author.ID, name, category, location, in.Condition, author.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result = tooling.Tool{ID: id, AuthorID: author.ID, Name: name, Category: category, Location: location, Condition: in.Condition, Signature: core.Signature{Name: author.Name, At: now}, CreatedAt: now, UpdatedAt: now, Version: 1}
		return appendAudit(ctx, tx, actor, "tool.create", "tool", id, nil, result, now)
	})
	if err != nil {
		return tooling.Tool{}, err
	}
	return result, nil
}
func (s *Store) UpdateTool(ctx context.Context, actorID, id int64, in tooling.ToolUpdate) error {
	name, category, location, err := toolText(in.Name, in.Category, in.Location, in.Condition)
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
		before, err := toolFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Name == name && before.Category == category && before.Location == location && before.Condition == in.Condition {
			return nil
		}
		after := before
		after.Name = name
		after.Category = category
		after.Location = location
		after.Condition = in.Condition
		after.Version++
		after.UpdatedAt = time.Now().UTC()
		r, err := tx.ExecContext(ctx, "UPDATE tools SET name=?,category=?,location=?,condition=?,updated_at=?,version=? WHERE id=? AND version=?", name, category, location, in.Condition, timestamp(after.UpdatedAt), after.Version, id, in.Version)
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
		return appendAudit(ctx, tx, actor, "tool.update", "tool", id, before, after, after.UpdatedAt)
	})
}

// ToolHistory exposes only events belonging to this inventory item and its
// immutable report/purchase references. It never returns general audit data.
func (s *Store) ToolHistory(ctx context.Context, toolID int64) ([]core.AuditEvent, error) {
	result := make([]core.AuditEvent, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := toolFrom(ctx, tx, toolID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `WITH reports AS (
   SELECT r.entry_id FROM tool_reports r JOIN entries e ON e.id=r.entry_id WHERE r.tool_id=? AND e.module='werkzeugmeldung'
  ), wishes AS (
   SELECT p.entry_id FROM purchases p JOIN entries e ON e.id=p.entry_id WHERE p.tool_id=? AND e.module='anschaffung'
  ), related_comments AS (
   SELECT id FROM comments WHERE entry_id IN (SELECT entry_id FROM reports UNION SELECT entry_id FROM wishes)
  )
  SELECT id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json FROM audit_log
  WHERE (entity_type='tool' AND entity_id=? AND action IN ('tool.create','tool.update'))
   OR (entity_type='tool_report' AND entity_id IN (SELECT entry_id FROM reports) AND action IN ('tool_report.create','tool_report.update','tool_report.priority'))
   OR (entity_type='purchase' AND entity_id IN (SELECT entry_id FROM wishes) AND action IN ('purchase.create','purchase.update','purchase.priority'))
   OR (entity_type='comment' AND entity_id IN (SELECT id FROM related_comments) AND action='comment.create')
  ORDER BY id DESC`, toolID, toolID, toolID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e core.AuditEvent
			var at string
			if err = rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.EntityType, &e.EntityID, &at, &e.Before, &e.After); err != nil {
				return err
			}
			if e.At, err = parseTime(at); err != nil {
				return err
			}
			result = append(result, e)
		}
		return rows.Err()
	})
	return result, err
}
