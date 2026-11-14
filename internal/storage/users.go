package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"werkstatt-hub/internal/core"
)

// BootstrapAdmin creates the first user only; subsequent administration requires
// an active admin actor and follows the same audit path as entry changes.
func (s *Store) BootstrapAdmin(ctx context.Context, name string) (core.User, error) {
	name, err := cleanName(name)
	if err != nil {
		return core.User{}, err
	}
	var result core.User
	err = s.write(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return core.ErrAlreadyInitialized
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO users(name,role,active) VALUES(?,?,1)", name, core.RoleAdmin)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result = core.User{ID: id, Name: name, Role: core.RoleAdmin, Active: true}
		return appendAudit(ctx, tx, result, "bootstrap_admin", "user", id, nil, result, time.Now())
	})
	if err != nil {
		return core.User{}, err
	}
	return result, nil
}
func (s *Store) CreateUser(ctx context.Context, actorID int64, name string, role core.Role) (core.User, error) {
	name, err := cleanName(name)
	if err != nil {
		return core.User{}, err
	}
	if !role.Valid() {
		return core.User{}, core.ErrValidation
	}
	var result core.User
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanAdmin(actor) {
			return core.ErrForbidden
		}
		if err = uniqueName(ctx, tx, name, 0); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO users(name,role,active) VALUES(?,?,1)", name, role)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		result = core.User{ID: id, Name: name, Role: role, Active: true}
		return appendAudit(ctx, tx, actor, "user.create", "user", id, nil, result, time.Now())
	})
	if err != nil {
		return core.User{}, err
	}
	return result, nil
}
func (s *Store) UpdateUser(ctx context.Context, actorID, id int64, name string, role core.Role, active bool) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	if !role.Valid() {
		return core.ErrValidation
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !core.CanAdmin(actor) {
			return core.ErrForbidden
		}
		before, err := userFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Name == name && before.Role == role && before.Active == active {
			return nil
		}
		if err = uniqueName(ctx, tx, name, id); err != nil {
			return err
		}
		if before.Role == core.RoleAdmin && before.Active && (!active || role != core.RoleAdmin) {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role=? AND active=1 AND id<>?", core.RoleAdmin, id).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("Letzter aktiver Admin muss erhalten bleiben: %w", core.ErrValidation)
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET name=?,role=?,active=? WHERE id=?", name, role, active, id); err != nil {
			return err
		}
		after := core.User{ID: id, Name: name, Role: role, Active: active}
		return appendAudit(ctx, tx, actor, "user.update", "user", id, before, after, time.Now())
	})
}
