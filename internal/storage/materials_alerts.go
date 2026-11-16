package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

const materialAlertColumns = `a.id,a.material_id,a.material_name,a.unit,a.quantity,a.minimum,a.author_id,a.signature_name,a.signature_at,a.acknowledged_by,a.acknowledged_name,a.acknowledged_at`

func scanMaterialAlert(row scanner) (materials.Alert, error) {
	var a materials.Alert
	var signed string
	var ackID sql.NullInt64
	var ackName, ackAt sql.NullString
	err := row.Scan(&a.ID, &a.MaterialID, &a.MaterialName, &a.Unit, &a.Quantity, &a.Minimum, &a.AuthorID, &a.Signature.Name, &signed, &ackID, &ackName, &ackAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, core.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if a.Signature.At, err = parseTime(signed); err != nil {
		return a, err
	}
	if ackID.Valid {
		a.Acknowledgment = &materials.Acknowledgment{UserID: ackID.Int64, Signature: core.Signature{Name: ackName.String}}
		a.Acknowledgment.Signature.At, err = parseTime(ackAt.String)
	}
	return a, err
}
func materialAlertFrom(ctx context.Context, q queryer, materialID, alertID int64) (materials.Alert, error) {
	return scanMaterialAlert(q.QueryRowContext(ctx, "SELECT "+materialAlertColumns+" FROM material_alerts a WHERE a.material_id=? AND a.id=?", materialID, alertID))
}
func openMaterialAlert(ctx context.Context, tx *sql.Tx, actor core.User, m materials.Material, now time.Time) (materials.Alert, error) {
	r, err := tx.ExecContext(ctx, `INSERT INTO material_alerts(material_id,material_name,unit,quantity,minimum,author_id,signature_name,signature_at) VALUES(?,?,?,?,?,?,?,?)`, m.ID, m.Name, m.Unit, m.Stock, m.Minimum, actor.ID, actor.Name, timestamp(now))
	if err != nil {
		return materials.Alert{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return materials.Alert{}, err
	}
	alert, err := materialAlertFrom(ctx, tx, m.ID, id)
	if err != nil {
		return materials.Alert{}, err
	}
	if err = appendAudit(ctx, tx, actor, "material.alert_open", "material_alert", id, nil, alert, now); err != nil {
		return materials.Alert{}, err
	}
	return alert, nil
}
func (s *Store) MaterialAlerts(ctx context.Context, materialID int64) ([]materials.Alert, error) {
	if _, err := s.Material(ctx, materialID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+materialAlertColumns+" FROM material_alerts a WHERE a.material_id=? ORDER BY a.id DESC", materialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]materials.Alert, 0)
	for rows.Next() {
		alert, err := scanMaterialAlert(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, alert)
	}
	return result, rows.Err()
}
func (s *Store) AcknowledgeMaterialAlert(ctx context.Context, actorID, materialID, alertID int64) (materials.Alert, error) {
	var result materials.Alert
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !materials.CanAcknowledge(actor) {
			return core.ErrForbidden
		}
		before, err := materialAlertFrom(ctx, tx, materialID, alertID)
		if err != nil {
			return err
		}
		if before.Acknowledgment != nil {
			return core.ErrConflict
		}
		now := time.Now().UTC()
		if _, err = tx.ExecContext(ctx, "UPDATE material_alerts SET acknowledged_by=?,acknowledged_name=?,acknowledged_at=? WHERE id=?", actor.ID, actor.Name, timestamp(now), alertID); err != nil {
			return err
		}
		// Warning state is part of the material version shown by count/edit forms.
		if _, err = tx.ExecContext(ctx, "UPDATE materials SET updated_at=?,version=version+1 WHERE id=?", timestamp(now), materialID); err != nil {
			return err
		}
		result, err = materialAlertFrom(ctx, tx, materialID, alertID)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "material.alert_acknowledge", "material_alert", alertID, before, result, now)
	})
	if err != nil {
		return materials.Alert{}, err
	}
	return result, nil
}

type MaterialDashboard struct {
	OpenCount int
	Alerts    []materials.Alert
}

func (s *Store) MaterialDashboard(ctx context.Context) (MaterialDashboard, error) {
	result := MaterialDashboard{Alerts: make([]materials.Alert, 0)}
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM material_alerts a JOIN materials m ON m.id=a.material_id WHERE m.active=1 AND a.acknowledged_at IS NULL").Scan(&result.OpenCount)
	if err != nil {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+materialAlertColumns+" FROM material_alerts a JOIN materials m ON m.id=a.material_id WHERE m.active=1 AND a.acknowledged_at IS NULL ORDER BY a.id LIMIT 5")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		alert, err := scanMaterialAlert(rows)
		if err != nil {
			return result, err
		}
		result.Alerts = append(result.Alerts, alert)
	}
	return result, rows.Err()
}
