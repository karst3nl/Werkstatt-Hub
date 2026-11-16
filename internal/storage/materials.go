package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/materials"
)

const materialColumns = `m.id,m.author_id,m.name,m.unit,m.location,m.stock,m.minimum,m.active,m.signature_name,m.signature_at,m.created_at,m.updated_at,m.version,
 coalesce((SELECT a.id FROM material_alerts a WHERE a.material_id=m.id AND a.acknowledged_at IS NULL),0)`

func scanMaterial(row scanner) (materials.Material, error) {
	var m materials.Material
	var signed, created, updated string
	err := row.Scan(&m.ID, &m.AuthorID, &m.Name, &m.Unit, &m.Location, &m.Stock, &m.Minimum, &m.Active, &m.Signature.Name, &signed, &created, &updated, &m.Version, &m.OpenAlertID)
	if errors.Is(err, sql.ErrNoRows) {
		return m, core.ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if m.Signature.At, err = parseTime(signed); err != nil {
		return m, err
	}
	if m.CreatedAt, err = parseTime(created); err != nil {
		return m, err
	}
	m.UpdatedAt, err = parseTime(updated)
	return m, err
}
func materialFrom(ctx context.Context, q queryer, id int64) (materials.Material, error) {
	return scanMaterial(q.QueryRowContext(ctx, "SELECT "+materialColumns+" FROM materials m WHERE m.id=?", id))
}
func (s *Store) Material(ctx context.Context, id int64) (materials.Material, error) {
	return materialFrom(ctx, s.db, id)
}

func cleanMaterialInput(in materials.Input) (materials.Input, error) {
	var err error
	if in.Name, err = cleanText(in.Name, 200); err != nil {
		return in, err
	}
	if in.Unit, err = cleanText(in.Unit, 50); err != nil {
		return in, err
	}
	if in.Location, err = optionalToolText(in.Location, 200); err != nil {
		return in, err
	}
	if !materials.ValidQuantity(in.Stock) || !materials.ValidQuantity(in.Minimum) {
		return in, core.ErrValidation
	}
	return in, nil
}

func (s *Store) CreateMaterial(ctx context.Context, actorID int64, in materials.Input) (materials.Material, error) {
	var result materials.Material
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !materials.CanManage(actor) {
			return core.ErrForbidden
		}
		in, err = cleanMaterialInput(in)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		r, err := tx.ExecContext(ctx, `INSERT INTO materials(author_id,name,unit,location,stock,minimum,active,signature_name,signature_at,created_at,updated_at,version) VALUES(?,?,?,?,?,?,1,?,?,?,?,1)`, actor.ID, in.Name, in.Unit, in.Location, in.Stock, in.Minimum, actor.Name, timestamp(now), timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result, err = materialFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if materials.IsLow(result.Stock, result.Minimum) {
			if _, err = openMaterialAlert(ctx, tx, actor, result, now); err != nil {
				return err
			}
			result, err = materialFrom(ctx, tx, id)
			if err != nil {
				return err
			}
		}
		return appendAudit(ctx, tx, actor, "material.create", "material", id, nil, result, now)
	})
	if err != nil {
		return materials.Material{}, err
	}
	return result, nil
}

func (s *Store) UpdateMaterial(ctx context.Context, actorID, id int64, in materials.Update) (materials.Material, error) {
	var result materials.Material
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !materials.CanManage(actor) {
			return core.ErrForbidden
		}
		before, err := materialFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if in.Version < 1 {
			return core.ErrValidation
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		clean, err := cleanMaterialInput(materials.Input{Name: in.Name, Unit: before.Unit, Location: in.Location, Stock: before.Stock, Minimum: in.Minimum})
		if err != nil {
			return err
		}
		result = before
		result.Name, result.Location, result.Minimum, result.Active = clean.Name, clean.Location, clean.Minimum, in.Active
		if reflect.DeepEqual(before, result) {
			return nil
		}
		now := time.Now().UTC()
		result.UpdatedAt, result.Version = now, before.Version+1
		if _, err = tx.ExecContext(ctx, "UPDATE materials SET name=?,location=?,minimum=?,active=?,updated_at=?,version=? WHERE id=?", result.Name, result.Location, result.Minimum, result.Active, timestamp(now), result.Version, id); err != nil {
			return err
		}
		// Only a threshold crossing starts a new warning after an earlier acknowledgement.
		if before.OpenAlertID == 0 && before.Stock > before.Minimum && materials.IsLow(result.Stock, result.Minimum) {
			if _, err = openMaterialAlert(ctx, tx, actor, result, now); err != nil {
				return err
			}
			result, err = materialFrom(ctx, tx, id)
			if err != nil {
				return err
			}
		}
		return appendAudit(ctx, tx, actor, "material.update", "material", id, before, result, now)
	})
	if err != nil {
		return materials.Material{}, err
	}
	return result, nil
}

func (s *Store) Materials(ctx context.Context, filter materials.Filter) ([]materials.Material, error) {
	if filter.Activity != "" && filter.Activity != "aktiv" && filter.Activity != "inaktiv" && filter.Activity != "alle" {
		return nil, core.ErrValidation
	}
	if filter.Status != "" && filter.Status != materials.FilterStatusLow {
		return nil, core.ErrValidation
	}
	search, err := optionalToolText(filter.Search, 200)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + materialColumns + " FROM materials m WHERE 1=1"
	if filter.Activity == "" || filter.Activity == "aktiv" {
		query += " AND m.active=1"
	} else if filter.Activity == "inaktiv" {
		query += " AND m.active=0"
	}
	if filter.Status == materials.FilterStatusLow {
		query += " AND m.stock<=m.minimum"
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY m.name COLLATE NOCASE,m.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]materials.Material, 0)
	for rows.Next() {
		m, err := scanMaterial(rows)
		if err != nil {
			return nil, err
		}
		if search == "" || strings.Contains(strings.ToLower(m.Name+" "+m.Location), strings.ToLower(search)) {
			result = append(result, m)
		}
	}
	return result, rows.Err()
}
