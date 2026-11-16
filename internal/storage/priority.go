package storage

import (
	"context"
	"database/sql"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

func equalPriority(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// SetPriority writes exactly one side. The independent value and the original
// signature survive; the shared version also protects simultaneous text edits.
func (s *Store) SetPriority(ctx context.Context, actorID, entryID int64, in core.PriorityChange) error {
	if in.Version < 1 || in.Side != core.PriorityEmployee && in.Side != core.PrioritySupervisor {
		return core.ErrValidation
	}
	var value *int
	if in.Value != nil {
		n := *in.Value
		if n < s.priorityScale.Min || n > s.priorityScale.Max {
			return core.ErrValidation
		}
		value = &n
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := entryFrom(ctx, tx, entryID)
		if err != nil {
			return err
		}
		if in.Side == core.PriorityEmployee && !core.CanSetEmployeePriority(actor, before) || in.Side == core.PrioritySupervisor && !core.CanSetSupervisorPriority(actor) {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		old := before.EmployeePriority
		column := "employee_priority"
		if in.Side == core.PrioritySupervisor {
			old = before.SupervisorPriority
			column = "supervisor_priority"
		}
		if equalPriority(old, value) {
			return nil
		}
		after := before
		after.UpdatedAt = time.Now().UTC()
		after.Version++
		if in.Side == core.PriorityEmployee {
			after.EmployeePriority = value
		} else {
			after.SupervisorPriority = value
		}
		var auditBefore, auditAfter any = before, after
		action, entity := "entry.priority", "entry"
		if before.Module == "idee" {
			previous, err := ideaFrom(ctx, tx, entryID)
			if err != nil {
				return err
			}
			auditBefore = previous
			auditAfter = ideas.Idea{Entry: after, Category: previous.Category}
			action, entity = "idea.priority", "idea"
		}
		if before.Module == "aufgabe" {
			previous, err := taskFrom(ctx, tx, entryID)
			if err != nil {
				return err
			}
			updated := previous
			updated.Entry = after
			auditBefore, auditAfter = previous, updated
			action, entity = "task.priority", "task"
		}
		if before.Module == "werkzeugmeldung" {
			previous, err := reportFrom(ctx, tx, entryID)
			if err != nil {
				return err
			}
			updated := previous
			updated.Entry = after
			auditBefore, auditAfter = previous, updated
			action, entity = "tool_report.priority", "tool_report"
		}
		if before.Module == "anschaffung" {
			previous, err := purchaseFrom(ctx, tx, entryID)
			if err != nil {
				return err
			}
			updated := previous
			updated.Entry = after
			auditBefore, auditAfter = previous, updated
			action, entity = "purchase.priority", "purchase"
		}
		if before.Module == "materialpruefung" {
			previous, err := materialCheckFrom(ctx, tx, entryID)
			if err != nil {
				return err
			}
			updated := previous
			updated.Entry = after
			auditBefore, auditAfter = previous, updated
			action, entity = "material_check.priority", "material_check"
		}
		r, err := tx.ExecContext(ctx, "UPDATE entries SET "+column+"=?,updated_at=?,version=? WHERE id=? AND version=?", value, timestamp(after.UpdatedAt), after.Version, entryID, in.Version)
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
		return appendAudit(ctx, tx, actor, action, entity, entryID, auditBefore, auditAfter, after.UpdatedAt)
	})
}
