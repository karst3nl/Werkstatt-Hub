package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"werkstatt-hub/internal/auth"
	"werkstatt-hub/internal/core"
)

// InitialAdmin contains a one-time secret for the local server console only.
// JSON and audit serialization deliberately omit the password.
type InitialAdmin struct {
	User     core.User
	Password string `json:"-"`
}

func (a InitialAdmin) String() string {
	return fmt.Sprintf("initial admin %q (password omitted)", a.User.Name)
}

type AdminAuth struct {
	User       core.User
	MustChange bool
}

type credentialAudit struct {
	UserID      int64 `json:"user_id"`
	Configured  bool  `json:"configured"`
	MustChange  bool  `json:"must_change_password"`
	AuthVersion int64 `json:"auth_version"`
}

func adminFrom(ctx context.Context, q queryer, id int64) (core.User, error) {
	u, err := actorFrom(ctx, q, id)
	if err != nil {
		return core.User{}, err
	}
	if !core.CanAdmin(u) {
		return core.User{}, core.ErrForbidden
	}
	return u, nil
}
func credentialFrom(ctx context.Context, q queryer, id int64) (string, bool, error) {
	var hash string
	var mustChange bool
	err := q.QueryRowContext(ctx, "SELECT password_hash,must_change FROM admin_credentials WHERE user_id=?", id).Scan(&hash, &mustChange)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, core.ErrForbidden
	}
	return hash, mustChange, err
}
func initialPasswordHash(role core.Role, passwords []string) (string, error) {
	if len(passwords) > 1 {
		return "", core.ErrValidation
	}
	if len(passwords) == 0 || passwords[0] == "" {
		return "", nil
	}
	if role != core.RoleAdmin {
		return "", core.ErrValidation
	}
	if err := auth.ValidatePassword(passwords[0]); err != nil {
		return "", fmt.Errorf("Neues Admin-Passwort: %w", core.ErrValidation)
	}
	return auth.HashPassword(passwords[0])
}
func setInitialCredential(ctx context.Context, tx *sql.Tx, actor, before, after core.User, hash, action string) error {
	var oldMustChange bool
	var oldConfigured bool
	err := tx.QueryRowContext(ctx, "SELECT must_change FROM admin_credentials WHERE user_id=?", after.ID).Scan(&oldMustChange)
	if err == nil {
		oldConfigured = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_credentials(user_id,password_hash,must_change) VALUES(?,?,1)
 ON CONFLICT(user_id) DO UPDATE SET password_hash=excluded.password_hash,must_change=1`, after.ID, hash); err != nil {
		return err
	}
	oldState := credentialAudit{UserID: after.ID, Configured: oldConfigured, MustChange: oldMustChange, AuthVersion: before.AuthVersion}
	newState := credentialAudit{UserID: after.ID, Configured: true, MustChange: true, AuthVersion: after.AuthVersion}
	return appendAudit(ctx, tx, actor, action, "user", after.ID, oldState, newState, time.Now())
}

// EnsureAdminAccess is invoked when the server starts, after migrations. It
// provisions missing credentials once and never resets an existing password.
func (s *Store) EnsureAdminAccess(ctx context.Context) ([]InitialAdmin, error) {
	result := make([]InitialAdmin, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			r, err := tx.ExecContext(ctx, "INSERT INTO users(name,role,active) VALUES('Admin',?,1)", core.RoleAdmin)
			if err != nil {
				return err
			}
			id, err := r.LastInsertId()
			if err != nil {
				return err
			}
			u := core.User{ID: id, Name: "Admin", Role: core.RoleAdmin, Active: true, AuthVersion: 1}
			if err = appendAudit(ctx, tx, u, "bootstrap_admin", "user", id, nil, u, time.Now()); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,name,role,active,deleted,auth_version FROM users WHERE role=? AND active=1 AND deleted=0 ORDER BY id", core.RoleAdmin)
		if err != nil {
			return err
		}
		users := make([]core.User, 0)
		for rows.Next() {
			var u core.User
			if err = rows.Scan(&u.ID, &u.Name, &u.Role, &u.Active, &u.Deleted, &u.AuthVersion); err != nil {
				rows.Close()
				return err
			}
			users = append(users, u)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if len(users) == 0 {
			return fmt.Errorf("Kein aktiver Admin vorhanden: %w", core.ErrValidation)
		}
		for _, u := range users {
			var configured int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_credentials WHERE user_id=?", u.ID).Scan(&configured); err != nil {
				return err
			}
			if configured != 0 {
				continue
			}
			password, err := auth.GeneratePassword()
			if err != nil {
				return err
			}
			hash, err := auth.HashPassword(password)
			if err != nil {
				return err
			}
			before := u
			u.AuthVersion++
			if _, err = tx.ExecContext(ctx, "UPDATE users SET auth_version=? WHERE id=?", u.AuthVersion, u.ID); err != nil {
				return err
			}
			if err = setInitialCredential(ctx, tx, u, before, u, hash, "admin.password_initialize"); err != nil {
				return err
			}
			result = append(result, InitialAdmin{User: u, Password: password})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Store) AdminAccess(ctx context.Context, userID int64) (AdminAuth, error) {
	var result AdminAuth
	err := s.write(ctx, func(tx *sql.Tx) error {
		u, err := adminFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		_, mustChange, err := credentialFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		result = AdminAuth{User: u, MustChange: mustChange}
		return nil
	})
	return result, err
}
func (s *Store) AuthenticateAdmin(ctx context.Context, userID int64, password string) (AdminAuth, error) {
	var result AdminAuth
	err := s.write(ctx, func(tx *sql.Tx) error {
		u, err := adminFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		hash, mustChange, err := credentialFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !auth.VerifyPassword(hash, password) {
			return core.ErrForbidden
		}
		result = AdminAuth{User: u, MustChange: mustChange}
		return nil
	})
	return result, err
}
func (s *Store) ChangeAdminPassword(ctx context.Context, userID int64, currentPassword, newPassword string) (AdminAuth, error) {
	if err := auth.ValidatePassword(newPassword); err != nil {
		return AdminAuth{}, core.ErrValidation
	}
	var result AdminAuth
	err := s.write(ctx, func(tx *sql.Tx) error {
		u, err := adminFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		hash, mustChange, err := credentialFrom(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !auth.VerifyPassword(hash, currentPassword) {
			return core.ErrForbidden
		}
		if currentPassword == newPassword {
			return core.ErrValidation
		}
		newHash, err := auth.HashPassword(newPassword)
		if err != nil {
			return err
		}
		before := credentialAudit{UserID: u.ID, Configured: true, MustChange: mustChange, AuthVersion: u.AuthVersion}
		u.AuthVersion++
		if _, err = tx.ExecContext(ctx, "UPDATE admin_credentials SET password_hash=?,must_change=0 WHERE user_id=?", newHash, u.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET auth_version=? WHERE id=?", u.AuthVersion, u.ID); err != nil {
			return err
		}
		after := credentialAudit{UserID: u.ID, Configured: true, MustChange: false, AuthVersion: u.AuthVersion}
		if err = appendAudit(ctx, tx, u, "admin.password_change", "user", u.ID, before, after, time.Now()); err != nil {
			return err
		}
		result = AdminAuth{User: u, MustChange: false}
		return nil
	})
	return result, err
}
