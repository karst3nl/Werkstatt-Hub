package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tooling"
)

type toolingScanner struct {
	scanner
	fields []any
}

func (s toolingScanner) Scan(dest ...any) error { return s.scanner.Scan(append(dest, s.fields...)...) }
func reportColumns() string {
	return "e." + strings.ReplaceAll(entryColumns, ",", ",e.") + ",r.tool_id,t.name,r.observed_condition"
}

const reportJoin = " FROM entries e JOIN tool_reports r ON r.entry_id=e.id JOIN tools t ON t.id=r.tool_id WHERE e.module='werkzeugmeldung'"

func scanToolReport(row scanner) (tooling.Report, error) {
	var report tooling.Report
	e, err := scanEntry(toolingScanner{row, []any{&report.ToolID, &report.ToolName, &report.ObservedCondition}})
	report.Entry = e
	return report, err
}
func reportFrom(ctx context.Context, q queryer, id int64) (tooling.Report, error) {
	return scanToolReport(q.QueryRowContext(ctx, "SELECT "+reportColumns()+reportJoin+" AND e.id=?", id))
}
func (s *Store) ToolReport(ctx context.Context, id int64) (tooling.Report, error) {
	return reportFrom(ctx, s.db, id)
}
func purchaseColumns() string {
	return "e." + strings.ReplaceAll(entryColumns, ",", ",e.") + ",p.tool_id,t.name,p.category,p.location"
}

const purchaseJoin = " FROM entries e JOIN purchases p ON p.entry_id=e.id LEFT JOIN tools t ON t.id=p.tool_id WHERE e.module='anschaffung'"

func scanPurchase(row scanner) (tooling.Purchase, error) {
	var purchase tooling.Purchase
	var toolID sql.NullInt64
	var toolName sql.NullString
	e, err := scanEntry(toolingScanner{row, []any{&toolID, &toolName, &purchase.Category, &purchase.Location}})
	purchase.Entry = e
	if toolID.Valid {
		purchase.ToolID = toolID.Int64
	}
	if toolName.Valid {
		purchase.ToolName = toolName.String
	}
	return purchase, err
}
func purchaseFrom(ctx context.Context, q queryer, id int64) (tooling.Purchase, error) {
	return scanPurchase(q.QueryRowContext(ctx, "SELECT "+purchaseColumns()+purchaseJoin+" AND e.id=?", id))
}
func (s *Store) Purchase(ctx context.Context, id int64) (tooling.Purchase, error) {
	return purchaseFrom(ctx, s.db, id)
}

type toolingPriorities struct {
	sort                                      string
	employee, supervisor                      *int
	employeeUnset, supervisorUnset, divergent bool
}

func (s *Store) toolingPriorityQuery(query string, args []any, f toolingPriorities) (string, []any, error) {
	switch f.sort {
	case "", "neueste", "mitarbeiter", "vorgesetzter":
	default:
		return "", nil, core.ErrValidation
	}
	if f.employeeUnset && f.employee != nil || f.supervisorUnset && f.supervisor != nil {
		return "", nil, core.ErrValidation
	}
	for _, value := range []*int{f.employee, f.supervisor} {
		if value != nil && (*value < s.priorityScale.Min || *value > s.priorityScale.Max) {
			return "", nil, core.ErrValidation
		}
	}
	if f.employee != nil {
		query += " AND e.employee_priority=?"
		args = append(args, *f.employee)
	}
	if f.supervisor != nil {
		query += " AND e.supervisor_priority=?"
		args = append(args, *f.supervisor)
	}
	if f.employeeUnset {
		query += " AND e.employee_priority IS NULL"
	}
	if f.supervisorUnset {
		query += " AND e.supervisor_priority IS NULL"
	}
	if f.divergent {
		query += " AND e.employee_priority IS NOT NULL AND e.supervisor_priority IS NOT NULL AND abs(e.employee_priority-e.supervisor_priority)>=?"
		args = append(args, (s.priorityScale.Max-s.priorityScale.Min)/2+1)
	}
	switch f.sort {
	case "mitarbeiter":
		query += " ORDER BY e.employee_priority DESC NULLS LAST,e.id DESC"
	case "vorgesetzter":
		query += " ORDER BY e.supervisor_priority DESC NULLS LAST,e.id DESC"
	default:
		query += " ORDER BY e.id DESC"
	}
	return query, args, nil
}
func (s *Store) ToolReports(ctx context.Context, f tooling.ReportFilter) ([]tooling.Report, error) {
	if f.ToolID < 0 || f.Status != "" && !tooling.ValidReportStatus(f.Status) {
		return nil, core.ErrValidation
	}
	query := "SELECT " + reportColumns() + reportJoin
	args := []any{}
	if f.ToolID > 0 {
		query += " AND r.tool_id=?"
		args = append(args, f.ToolID)
	}
	if f.Status != "" {
		query += " AND e.status=?"
		args = append(args, f.Status)
	}
	query, args, err := s.toolingPriorityQuery(query, args, toolingPriorities{f.Sort, f.EmployeePriority, f.SupervisorPriority, f.EmployeeUnset, f.SupervisorUnset, f.OnlyDivergent})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]tooling.Report, 0)
	for rows.Next() {
		r, err := scanToolReport(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Store) Purchases(ctx context.Context, f tooling.PurchaseFilter) ([]tooling.Purchase, error) {
	if f.ToolID < 0 || f.Status != "" && !tooling.ValidPurchaseStatus(f.Status) {
		return nil, core.ErrValidation
	}
	var err error
	f.Category, err = optionalCategory(f.Category)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + purchaseColumns() + purchaseJoin
	args := []any{}
	if f.ToolID > 0 {
		query += " AND p.tool_id=?"
		args = append(args, f.ToolID)
	}
	if f.Status != "" {
		query += " AND e.status=?"
		args = append(args, f.Status)
	}
	if f.Category != "" {
		query += " AND p.category=?"
		args = append(args, f.Category)
	}
	query, args, err = s.toolingPriorityQuery(query, args, toolingPriorities{f.Sort, f.EmployeePriority, f.SupervisorPriority, f.EmployeeUnset, f.SupervisorUnset, f.OnlyDivergent})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]tooling.Purchase, 0)
	for rows.Next() {
		p, err := scanPurchase(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) PurchaseCategories(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT p.category FROM purchases p JOIN entries e ON e.id=p.entry_id WHERE e.module='anschaffung' ORDER BY p.category COLLATE NOCASE,p.category")
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
func toolingText(title, body string) (string, string, error) {
	title, err := cleanText(title, 200)
	if err != nil {
		return "", "", err
	}
	body, err = cleanText(body, 20000)
	return title, body, err
}
func toolReference(ctx context.Context, q queryer, id int64) (tooling.Tool, error) {
	if id < 1 {
		return tooling.Tool{}, core.ErrValidation
	}
	t, err := toolFrom(ctx, q, id)
	if errors.Is(err, core.ErrNotFound) {
		return tooling.Tool{}, core.ErrValidation
	}
	return t, err
}
func insertToolingEntry(ctx context.Context, tx *sql.Tx, actor core.User, module, status, title, body string) (core.Entry, error) {
	now := time.Now().UTC()
	r, err := tx.ExecContext(ctx, `INSERT INTO entries(module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,?,?,1)`, module, actor.ID, title, body, status, actor.Name, timestamp(now), timestamp(now), timestamp(now))
	if err != nil {
		return core.Entry{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return core.Entry{}, err
	}
	return core.Entry{ID: id, Module: module, AuthorID: actor.ID, Title: title, Body: body, Status: status, Signature: core.Signature{Name: actor.Name, At: now}, CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}
func updateToolingEntry(ctx context.Context, tx *sql.Tx, after core.Entry, beforeVersion int64) error {
	r, err := tx.ExecContext(ctx, "UPDATE entries SET title=?,body=?,status=?,updated_at=?,version=? WHERE id=? AND version=?", after.Title, after.Body, after.Status, timestamp(after.UpdatedAt), after.Version, after.ID, beforeVersion)
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
	return nil
}
func (s *Store) CreateToolReport(ctx context.Context, authorID int64, in tooling.ReportInput) (tooling.Report, error) {
	title, body, err := toolingText(in.Title, in.Body)
	if err != nil {
		return tooling.Report{}, err
	}
	if !tooling.ValidCondition(in.ObservedCondition) || in.ToolID < 1 {
		return tooling.Report{}, core.ErrValidation
	}
	var result tooling.Report
	err = s.write(ctx, func(tx *sql.Tx) error {
		author, err := actorFrom(ctx, tx, authorID)
		if err != nil {
			return err
		}
		tool, err := toolReference(ctx, tx, in.ToolID)
		if err != nil {
			return err
		}
		e, err := insertToolingEntry(ctx, tx, author, "werkzeugmeldung", "neu", title, body)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tool_reports(entry_id,tool_id,observed_condition) VALUES(?,?,?)", e.ID, tool.ID, in.ObservedCondition); err != nil {
			return err
		}
		result = tooling.Report{Entry: e, ToolID: tool.ID, ToolName: tool.Name, ObservedCondition: in.ObservedCondition}
		return appendAudit(ctx, tx, author, "tool_report.create", "tool_report", e.ID, nil, result, e.CreatedAt)
	})
	if err != nil {
		return tooling.Report{}, err
	}
	return result, nil
}
func (s *Store) UpdateToolReport(ctx context.Context, actorID, id int64, in tooling.ReportUpdate) error {
	title, body, err := toolingText(in.Title, in.Body)
	if err != nil {
		return err
	}
	if in.Version < 1 || !tooling.ValidReportStatus(in.Status) || !tooling.ValidCondition(in.ObservedCondition) {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := reportFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if !core.CanEditEntry(actor, before.Entry) || !core.CanManage(actor) && in.Status != before.Status {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Title == title && before.Body == body && before.Status == in.Status && before.ObservedCondition == in.ObservedCondition {
			return nil
		}
		after := before
		after.Title = title
		after.Body = body
		after.Status = in.Status
		after.ObservedCondition = in.ObservedCondition
		after.Version++
		after.UpdatedAt = time.Now().UTC()
		if err = updateToolingEntry(ctx, tx, after.Entry, in.Version); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tool_reports SET observed_condition=? WHERE entry_id=?", after.ObservedCondition, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "tool_report.update", "tool_report", id, before, after, after.UpdatedAt)
	})
}
func purchaseText(title, body, category, location string) (string, string, string, string, error) {
	title, body, err := toolingText(title, body)
	if err != nil {
		return "", "", "", "", err
	}
	category, err = cleanName(category)
	if err != nil {
		return "", "", "", "", err
	}
	location, err = optionalToolText(location, 100)
	return title, body, category, location, err
}
func (s *Store) CreatePurchase(ctx context.Context, authorID int64, in tooling.PurchaseInput) (tooling.Purchase, error) {
	title, body, category, location, err := purchaseText(in.Title, in.Body, in.Category, in.Location)
	if err != nil {
		return tooling.Purchase{}, err
	}
	if in.ToolID < 0 {
		return tooling.Purchase{}, core.ErrValidation
	}
	var result tooling.Purchase
	err = s.write(ctx, func(tx *sql.Tx) error {
		author, err := actorFrom(ctx, tx, authorID)
		if err != nil {
			return err
		}
		var toolID any
		var toolName string
		if in.ToolID > 0 {
			tool, err := toolReference(ctx, tx, in.ToolID)
			if err != nil {
				return err
			}
			toolID, toolName = tool.ID, tool.Name
		}
		e, err := insertToolingEntry(ctx, tx, author, "anschaffung", "gewuenscht", title, body)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO purchases(entry_id,tool_id,category,location) VALUES(?,?,?,?)", e.ID, toolID, category, location); err != nil {
			return err
		}
		result = tooling.Purchase{Entry: e, ToolID: in.ToolID, ToolName: toolName, Category: category, Location: location}
		return appendAudit(ctx, tx, author, "purchase.create", "purchase", e.ID, nil, result, e.CreatedAt)
	})
	if err != nil {
		return tooling.Purchase{}, err
	}
	return result, nil
}
func (s *Store) UpdatePurchase(ctx context.Context, actorID, id int64, in tooling.PurchaseUpdate) error {
	title, body, category, location, err := purchaseText(in.Title, in.Body, in.Category, in.Location)
	if err != nil {
		return err
	}
	if in.Version < 1 || !tooling.ValidPurchaseStatus(in.Status) {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := purchaseFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if !tooling.CanEditPurchase(actor, before) || !core.CanManage(actor) && in.Status != before.Status {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		if before.Title == title && before.Body == body && before.Category == category && before.Location == location && before.Status == in.Status {
			return nil
		}
		after := before
		after.Title = title
		after.Body = body
		after.Category = category
		after.Location = location
		after.Status = in.Status
		after.Version++
		after.UpdatedAt = time.Now().UTC()
		if err = updateToolingEntry(ctx, tx, after.Entry, in.Version); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE purchases SET category=?,location=? WHERE entry_id=?", category, location, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "purchase.update", "purchase", id, before, after, after.UpdatedAt)
	})
}
