package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

func materialCheckColumns() string {
	return "e." + strings.ReplaceAll(entryColumns, ",", ",e.") + ",c.material_id,m.name,m.unit,c.quantity"
}

const materialCheckJoin = " FROM entries e JOIN material_checks c ON c.entry_id=e.id JOIN materials m ON m.id=c.material_id WHERE e.module='materialpruefung'"

func scanMaterialCheck(row scanner) (materials.Check, error) {
	var c materials.Check
	e, err := scanEntry(toolingScanner{row, []any{&c.MaterialID, &c.MaterialName, &c.Unit, &c.Quantity}})
	c.Entry = e
	return c, err
}
func materialCheckFrom(ctx context.Context, q queryer, id int64) (materials.Check, error) {
	return scanMaterialCheck(q.QueryRowContext(ctx, "SELECT "+materialCheckColumns()+materialCheckJoin+" AND e.id=?", id))
}
func (s *Store) MaterialCheck(ctx context.Context, id int64) (materials.Check, error) {
	return materialCheckFrom(ctx, s.db, id)
}
func (s *Store) MaterialChecks(ctx context.Context, materialID int64) ([]materials.Check, error) {
	if _, err := s.Material(ctx, materialID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+materialCheckColumns()+materialCheckJoin+" AND c.material_id=? ORDER BY e.id DESC", materialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]materials.Check, 0)
	for rows.Next() {
		c, err := scanMaterialCheck(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// RecordMaterialCheck accepts an explicitly selected active author and the
// material version that was shown alongside the count form. It issues no session.
func (s *Store) RecordMaterialCheck(ctx context.Context, authorID, materialID int64, in materials.CheckInput) (materials.Check, error) {
	var result materials.Check
	err := s.write(ctx, func(tx *sql.Tx) error {
		author, err := actorFrom(ctx, tx, authorID)
		if err != nil {
			return err
		}
		if !materials.CanCheck(author) {
			return core.ErrForbidden
		}
		if !materials.ValidQuantity(in.Quantity) || in.Version < 1 {
			return core.ErrValidation
		}
		note, err := optionalToolText(in.Note, 20000)
		if err != nil {
			return err
		}
		before, err := materialFrom(ctx, tx, materialID)
		if err != nil {
			return err
		}
		if !before.Active {
			return core.ErrForbidden
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		title := []rune("Bestandsprüfung: " + before.Name)
		if len(title) > 200 {
			title = title[:200]
		}
		if note == "" {
			note = "Geprüfter Bestand: " + materials.FormatQuantity(in.Quantity) + " " + before.Unit
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO entries(module,author_id,title,body,status,signature_name,signature_at,created_at,updated_at,version) VALUES('materialpruefung',?,?,?,'erfasst',?,?,?,?,1)`, author.ID, string(title), note, author.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO material_checks(entry_id,material_id,quantity) VALUES(?,?,?)", id, materialID, in.Quantity); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE materials SET stock=?,updated_at=?,version=version+1 WHERE id=?", in.Quantity, timestamp(now), materialID); err != nil {
			return err
		}
		after, err := materialFrom(ctx, tx, materialID)
		if err != nil {
			return err
		}
		if materials.ShouldWarn(before.Stock, in.Quantity, before.Minimum, before.OpenAlertID > 0) {
			if _, err = openMaterialAlert(ctx, tx, author, after, now); err != nil {
				return err
			}
			after, err = materialFrom(ctx, tx, materialID)
			if err != nil {
				return err
			}
		}
		result, err = materialCheckFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, author, "material_check.create", "material_check", id, before, struct {
			Check    materials.Check    `json:"check"`
			Material materials.Material `json:"material"`
		}{result, after}, now)
	})
	if err != nil {
		return materials.Check{}, err
	}
	return result, nil
}
