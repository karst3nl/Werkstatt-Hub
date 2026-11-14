package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"werkstatt-hub/internal/core"
)

// BootstrapAdmin is retained for trusted local setup tools. Server startup
// provisions its missing password via EnsureAdminAccess before accepting HTTP.
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
		result = core.User{ID: id, Name: name, Role: core.RoleAdmin, Active: true, AuthVersion: 1}
		return appendAudit(ctx, tx, result, "bootstrap_admin", "user", id, nil, result, time.Now())
	})
	if err != nil {
		return core.User{}, err
	}
	return result, nil
}

// CreateUser allows only a freshly loaded admin actor. A supplied temporary
// admin password is persisted in the same transaction as the new user.
func (s *Store) CreateUser(ctx context.Context, actorID int64, name string, role core.Role, initialPassword ...string) (core.User, error) {
	name, err := cleanName(name)
	if err != nil {
		return core.User{}, err
	}
	if !role.Valid() {
		return core.User{}, core.ErrValidation
	}
	hash, err := initialPasswordHash(role, initialPassword)
	if err != nil {
		return core.User{}, err
	}
	var result core.User
	err = s.write(ctx, func(tx *sql.Tx) error {
		actor, err := adminFrom(ctx, tx, actorID)
		if err != nil {
			return err
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
		result = core.User{ID: id, Name: name, Role: role, Active: true, AuthVersion: 1}
		if err = appendAudit(ctx, tx, actor, "user.create", "user", id, nil, result, time.Now()); err != nil {
			return err
		}
		if hash != "" {
			return setInitialCredential(ctx, tx, actor, core.User{}, result, hash, "admin.password_initialize")
		}
		return nil
	})
	if err != nil {
		return core.User{}, err
	}
	return result, nil
}
func lastAdminGuard(ctx context.Context, tx *sql.Tx, before core.User, role core.Role, active, deleted bool) error {
	if before.Role != core.RoleAdmin || !before.Active || before.Deleted || active && !deleted && role == core.RoleAdmin {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role=? AND active=1 AND deleted=0 AND id<>?", core.RoleAdmin, before.ID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("Letzter aktiver Admin muss erhalten bleiben: %w", core.ErrValidation)
	}
	return nil
}
func (s *Store) UpdateUser(ctx context.Context, actorID, id int64, name string, role core.Role, active bool, initialPassword ...string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	if !role.Valid() {
		return core.ErrValidation
	}
	hash, err := initialPasswordHash(role, initialPassword)
	if err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := adminFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := userFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Deleted {
			return core.ErrNotFound
		}
		// A legacy inactive admin was intentionally omitted by startup setup.
		// Reactivation must provision its first credential in this transaction.
		if before.Role == core.RoleAdmin && role == core.RoleAdmin && !before.Active && active && hash == "" {
			var configured int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_credentials WHERE user_id=?", id).Scan(&configured); err != nil {
				return err
			}
			if configured == 0 {
				return fmt.Errorf("Admin-Startpasswort erforderlich: %w", core.ErrValidation)
			}
		}
		profileChanged := before.Name != name || before.Role != role || before.Active != active
		if !profileChanged && hash == "" {
			return nil
		}
		if err = uniqueName(ctx, tx, name, id); err != nil {
			return err
		}
		if err = lastAdminGuard(ctx, tx, before, role, active, false); err != nil {
			return err
		}
		after := before
		after.Name = name
		after.Role = role
		after.Active = active
		if before.Role != role || before.Active != active || hash != "" {
			after.AuthVersion++
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET name=?,role=?,active=?,auth_version=? WHERE id=?", name, role, active, after.AuthVersion, id); err != nil {
			return err
		}
		if role != core.RoleAdmin {
			if _, err = tx.ExecContext(ctx, "DELETE FROM admin_credentials WHERE user_id=?", id); err != nil {
				return err
			}
		}
		if profileChanged {
			if err = appendAudit(ctx, tx, actor, "user.update", "user", id, before, after, time.Now()); err != nil {
				return err
			}
		}
		if hash != "" {
			return setInitialCredential(ctx, tx, actor, before, after, hash, "admin.password_reset")
		}
		return nil
	})
}

// DeleteUser removes the account from all selections while retaining the row
// required by historical authors, comments, task approvals and audit events.
func (s *Store) DeleteUser(ctx context.Context, actorID, id int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		actor, err := adminFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		before, err := userFrom(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Deleted {
			return core.ErrNotFound
		}
		if err = lastAdminGuard(ctx, tx, before, before.Role, false, true); err != nil {
			return err
		}
		after := before
		after.Active = false
		after.Deleted = true
		after.AuthVersion++
		if _, err = tx.ExecContext(ctx, "UPDATE users SET active=0,deleted=1,auth_version=? WHERE id=?", after.AuthVersion, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM admin_credentials WHERE user_id=?", id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "user.delete", "user", id, before, after, time.Now())
	})
}
