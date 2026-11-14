package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/wartung"
)

const wartungObjectColumns = `o.id,o.author_id,o.name,o.description,o.category,o.location,o.interval_value,o.interval_unit,
 max(o.baseline_date,coalesce((SELECT max(r.performed_date) FROM wartung_records r WHERE r.object_id=o.id),o.baseline_date)),
 o.warning_days,o.instructions,o.active,o.signature_name,o.signature_at,o.updated_at,o.version`

func scanWartungObject(row scanner) (wartung.Object, error) {
	var object wartung.Object
	var warning sql.NullInt64
	var signed, updated string
	err := row.Scan(&object.ID, &object.AuthorID, &object.Name, &object.Description, &object.Category, &object.Location,
		&object.IntervalValue, &object.IntervalUnit, &object.LastMaintenanceDate, &warning, &object.Instructions,
		&object.Active, &object.Signature.Name, &signed, &updated, &object.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return object, core.ErrNotFound
	}
	if err != nil {
		return object, err
	}
	if warning.Valid {
		value := int(warning.Int64)
		object.WarningDays = &value
	}
	if object.Signature.At, err = parseTime(signed); err != nil {
		return object, err
	}
	object.UpdatedAt, err = parseTime(updated)
	return object, err
}

func wartungObjectFrom(ctx context.Context, q queryer, id int64) (wartung.Object, error) {
	return scanWartungObject(q.QueryRowContext(ctx, "SELECT "+wartungObjectColumns+" FROM wartung_objects o WHERE o.id=?", id))
}

func (s *Store) WartungObject(ctx context.Context, id int64) (wartung.Object, error) {
	return wartungObjectFrom(ctx, s.db, id)
}

func wartungSettingsFrom(ctx context.Context, q queryer) (wartung.Settings, error) {
	var settings wartung.Settings
	err := q.QueryRowContext(ctx, "SELECT warning_days,version FROM wartung_settings WHERE id=1").Scan(&settings.WarningDays, &settings.Version)
	return settings, err
}

func (s *Store) WartungSettings(ctx context.Context) (wartung.Settings, error) {
	return wartungSettingsFrom(ctx, s.db)
}

func (s *Store) UpdateWartungSettings(ctx context.Context, actorID int64, warningDays int, version int64) (wartung.Settings, error) {
	var result wartung.Settings
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !wartung.CanManageObjects(actor) {
			return core.ErrForbidden
		}
		if !wartung.ValidWarningDays(warningDays) || version < 1 {
			return core.ErrValidation
		}
		before, err := wartungSettingsFrom(ctx, tx)
		if err != nil {
			return err
		}
		if before.Version != version {
			return core.ErrConflict
		}
		result = before
		if before.WarningDays == warningDays {
			return nil
		}
		result.WarningDays, result.Version = warningDays, before.Version+1
		if _, err = tx.ExecContext(ctx, "UPDATE wartung_settings SET warning_days=?,version=? WHERE id=1", result.WarningDays, result.Version); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "wartung.settings_update", "wartung_settings", 1, before, result, time.Now().UTC())
	})
	if err != nil {
		return wartung.Settings{}, err
	}
	return result, nil
}

func cleanWartungInput(in wartung.Input) (wartung.Input, error) {
	var err error
	if in.Name, err = cleanText(in.Name, 200); err != nil {
		return in, err
	}
	if in.Category, err = cleanName(in.Category); err != nil {
		return in, err
	}
	if in.Description, err = optionalToolText(in.Description, 20000); err != nil {
		return in, err
	}
	if in.Location, err = optionalToolText(in.Location, 200); err != nil {
		return in, err
	}
	if in.Instructions, err = optionalToolText(in.Instructions, 20000); err != nil {
		return in, err
	}
	if !wartung.ValidInterval(in.IntervalValue, in.IntervalUnit) || in.WarningDays != nil && !wartung.ValidWarningDays(*in.WarningDays) {
		return in, core.ErrValidation
	}
	return in, nil
}

// CreateWartungObject signs the immutable starting point with the acting admin.
func (s *Store) CreateWartungObject(ctx context.Context, actorID int64, in wartung.Input) (wartung.Object, error) {
	return s.createWartungObjectAt(ctx, actorID, in, time.Now().UTC())
}

func (s *Store) createWartungObjectAt(ctx context.Context, actorID int64, in wartung.Input, now time.Time) (wartung.Object, error) {
	var result wartung.Object
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !wartung.CanManageObjects(actor) {
			return core.ErrForbidden
		}
		in, err = cleanWartungInput(in)
		if err != nil {
			return err
		}
		if !wartung.ValidPerformedDate(in.LastMaintenanceDate, now, s.taskSettings.Location) {
			return core.ErrValidation
		}
		if _, err = wartung.NextDue(in.LastMaintenanceDate, in.IntervalValue, in.IntervalUnit); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO wartung_objects(author_id,name,description,category,location,interval_value,interval_unit,baseline_date,warning_days,instructions,active,signature_name,signature_at,updated_at,version)
 VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?,?,1)`, actor.ID, in.Name, in.Description, in.Category, in.Location, in.IntervalValue, in.IntervalUnit, in.LastMaintenanceDate, in.WarningDays, in.Instructions, actor.Name, timestamp(now), timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result, err = wartungObjectFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "wartung.object_create", "wartung_object", id, nil, result, now)
	})
	if err != nil {
		return wartung.Object{}, err
	}
	return result, nil
}

func (s *Store) UpdateWartungObject(ctx context.Context, actorID, id int64, in wartung.Update) (wartung.Object, error) {
	var result wartung.Object
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !wartung.CanManageObjects(actor) {
			return core.ErrForbidden
		}
		before, err := wartungObjectFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Version != in.Version {
			return core.ErrConflict
		}
		clean, err := cleanWartungInput(wartung.Input{Name: in.Name, Description: in.Description, Category: in.Category,
			Location: in.Location, IntervalValue: in.IntervalValue, IntervalUnit: in.IntervalUnit,
			LastMaintenanceDate: before.LastMaintenanceDate, WarningDays: in.WarningDays, Instructions: in.Instructions})
		if err != nil {
			return err
		}
		if _, err = wartung.NextDue(before.LastMaintenanceDate, clean.IntervalValue, clean.IntervalUnit); err != nil {
			return err
		}
		result = before
		result.Name, result.Description, result.Category, result.Location = clean.Name, clean.Description, clean.Category, clean.Location
		result.IntervalValue, result.IntervalUnit, result.WarningDays, result.Instructions, result.Active = clean.IntervalValue, clean.IntervalUnit, clean.WarningDays, clean.Instructions, in.Active
		if reflect.DeepEqual(before, result) {
			return nil
		}
		now := time.Now().UTC()
		result.UpdatedAt, result.Version = now, before.Version+1
		_, err = tx.ExecContext(ctx, `UPDATE wartung_objects SET name=?,description=?,category=?,location=?,interval_value=?,interval_unit=?,warning_days=?,instructions=?,active=?,updated_at=?,version=? WHERE id=?`,
			result.Name, result.Description, result.Category, result.Location, result.IntervalValue, result.IntervalUnit, result.WarningDays, result.Instructions, result.Active, timestamp(now), result.Version, id)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "wartung.object_update", "wartung_object", id, before, result, now)
	})
	if err != nil {
		return wartung.Object{}, err
	}
	return result, nil
}

func (s *Store) WartungObjects(ctx context.Context, filter wartung.Filter) ([]wartung.Object, error) {
	return s.wartungObjectsAt(ctx, filter, time.Now().UTC())
}

func (s *Store) wartungObjectsAt(ctx context.Context, filter wartung.Filter, now time.Time) ([]wartung.Object, error) {
	category, err := optionalCategory(filter.Category)
	if err != nil {
		return nil, err
	}
	if filter.Status != "" && filter.Status != wartung.StatusOK && filter.Status != wartung.StatusSoon && filter.Status != wartung.StatusOverdue {
		return nil, core.ErrValidation
	}
	if filter.Activity != "" && filter.Activity != "aktiv" && filter.Activity != "inaktiv" && filter.Activity != "alle" {
		return nil, core.ErrValidation
	}
	settings, err := s.WartungSettings(ctx)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + wartungObjectColumns + " FROM wartung_objects o WHERE 1=1"
	args := []any{}
	if category != "" {
		query += " AND o.category=?"
		args = append(args, category)
	}
	if filter.Activity == "" || filter.Activity == "aktiv" {
		query += " AND o.active=1"
	} else if filter.Activity == "inaktiv" {
		query += " AND o.active=0"
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY o.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objects := make([]wartung.Object, 0)
	for rows.Next() {
		object, err := scanWartungObject(rows)
		if err != nil {
			return nil, err
		}
		if filter.Status == "" || wartung.Status(object, settings.WarningDays, now, s.taskSettings.Location) == filter.Status {
			objects = append(objects, object)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(objects, func(i, j int) bool {
		a, _ := wartung.NextDue(objects[i].LastMaintenanceDate, objects[i].IntervalValue, objects[i].IntervalUnit)
		b, _ := wartung.NextDue(objects[j].LastMaintenanceDate, objects[j].IntervalValue, objects[j].IntervalUnit)
		return a < b
	})
	return objects, nil
}

func (s *Store) WartungCategories(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT category FROM wartung_objects ORDER BY category COLLATE NOCASE,category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var category string
		if err = rows.Scan(&category); err != nil {
			return nil, err
		}
		result = append(result, category)
	}
	return result, rows.Err()
}

type WartungDashboard struct {
	OverdueCount, SoonCount int
	Overdue, Soon           []wartung.Object
}

func (s *Store) WartungDashboard(ctx context.Context) (WartungDashboard, error) {
	return s.wartungDashboardAt(ctx, time.Now().UTC())
}

func (s *Store) wartungDashboardAt(ctx context.Context, now time.Time) (WartungDashboard, error) {
	objects, err := s.wartungObjectsAt(ctx, wartung.Filter{}, now)
	if err != nil {
		return WartungDashboard{}, err
	}
	settings, err := s.WartungSettings(ctx)
	if err != nil {
		return WartungDashboard{}, err
	}
	var summary WartungDashboard
	for _, object := range objects {
		switch wartung.Status(object, settings.WarningDays, now, s.taskSettings.Location) {
		case wartung.StatusOverdue:
			summary.OverdueCount++
			if len(summary.Overdue) < 5 {
				summary.Overdue = append(summary.Overdue, object)
			}
		case wartung.StatusSoon:
			summary.SoonCount++
			if len(summary.Soon) < 5 {
				summary.Soon = append(summary.Soon, object)
			}
		}
	}
	return summary, nil
}
