package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"werkstatt-hub/internal/core"
)

func appendAudit(ctx context.Context, tx *sql.Tx, actor core.User, action, entityType string, entityID int64, before, after any, at time.Time) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json)
  VALUES(?,?,?,?,?,?,?,?)`, actor.ID, actor.Name, action, entityType, entityID, timestamp(at), string(beforeJSON), string(afterJSON))
	return err
}
func (s *Store) Audit(ctx context.Context, actorID int64) ([]core.AuditEvent, error) {
	result := make([]core.AuditEvent, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanManage(actor) {
			return core.ErrForbidden
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,actor_id,actor_name,action,entity_type,entity_id,at,before_json,after_json FROM audit_log ORDER BY id DESC")
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
