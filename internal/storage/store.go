// Package storage persists shared models and applies role checks and audit writes
// together in a transaction. Callers provide an actor ID, never a trusted role.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"
	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/tasks"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db            *sql.DB
	priorityScale core.PriorityScale
	taskSettings  tasks.Settings
}

type Options struct {
	PriorityScale core.PriorityScale
	TaskSettings  tasks.Settings
}

func (s *Store) PriorityScale() core.PriorityScale { return s.priorityScale }
func (s *Store) TaskSettings() tasks.Settings      { return s.taskSettings }

func Open(path string) (*Store, error) { return OpenWithOptions(path, Options{}) }

func OpenWithOptions(path string, opts Options) (*Store, error) {
	if opts.PriorityScale == (core.PriorityScale{}) {
		opts.PriorityScale = core.DefaultPriorityScale()
	}
	if !opts.PriorityScale.Valid() {
		return nil, fmt.Errorf("Prioritätsskala: %w", core.ErrValidation)
	}
	if opts.TaskSettings == (tasks.Settings{}) {
		opts.TaskSettings = tasks.DefaultSettings()
	}
	if !opts.TaskSettings.Valid() {
		return nil, fmt.Errorf("Aufgabenerinnerungen: %w", core.ErrValidation)
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path: %w", core.ErrValidation)
	}
	var dsn string
	if path == ":memory:" {
		dsn = ":memory:?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			return nil, fmt.Errorf("database directory: %w", err)
		}
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("database file: %w", err)
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: abs}
		dsn = u.String() + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, priorityScale: opts.PriorityScale, taskSettings: opts.TaskSettings}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err = s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	var incompatible int
	if err = db.QueryRow(`SELECT count(*) FROM entries WHERE
 (employee_priority IS NOT NULL AND (employee_priority<? OR employee_priority>?)) OR
 (supervisor_priority IS NOT NULL AND (supervisor_priority<? OR supervisor_priority>?))`, s.priorityScale.Min, s.priorityScale.Max, s.priorityScale.Min, s.priorityScale.Max).Scan(&incompatible); err != nil {
		db.Close()
		return nil, err
	}
	if incompatible > 0 {
		db.Close()
		return nil, fmt.Errorf("Prioritätsskala schließt gespeicherte Werte aus: %w", core.ErrValidation)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func userFrom(ctx context.Context, q queryer, id int64) (core.User, error) {
	var u core.User
	err := q.QueryRowContext(ctx, "SELECT id,name,role,active,deleted,auth_version FROM users WHERE id=?", id).Scan(&u.ID, &u.Name, &u.Role, &u.Active, &u.Deleted, &u.AuthVersion)
	if errors.Is(err, sql.ErrNoRows) {
		err = core.ErrNotFound
	}
	return u, err
}
func actorFrom(ctx context.Context, q queryer, id int64) (core.User, error) {
	u, err := userFrom(ctx, q, id)
	if errors.Is(err, core.ErrNotFound) {
		return core.User{}, core.ErrForbidden
	}
	if err != nil {
		return core.User{}, err
	}
	if !u.Active || u.Deleted || !u.Role.Valid() {
		return core.User{}, core.ErrForbidden
	}
	return u, nil
}
func (s *Store) User(ctx context.Context, id int64) (core.User, error) {
	u, err := userFrom(ctx, s.db, id)
	if err == nil && u.Deleted {
		return core.User{}, core.ErrNotFound
	}
	return u, err
}
func (s *Store) Users(ctx context.Context) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,role,active,deleted,auth_version FROM users WHERE deleted=0 ORDER BY name COLLATE NOCASE,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]core.User, 0)
	for rows.Next() {
		var u core.User
		if err = rows.Scan(&u.ID, &u.Name, &u.Role, &u.Active, &u.Deleted, &u.AuthVersion); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func timestamp(t time.Time) string          { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }
func cleanText(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) == 0 || utf8.RuneCountInString(s) > max || strings.ContainsRune(s, 0) {
		return "", core.ErrValidation
	}
	return s, nil
}
func cleanName(s string) (string, error) {
	s, err := cleanText(s, 100)
	if err != nil {
		return "", err
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", core.ErrValidation
	}
	return s, nil
}
func uniqueName(ctx context.Context, tx *sql.Tx, name string, excludeID int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT id,name FROM users WHERE id<>? AND deleted=0", excludeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var other string
		if err = rows.Scan(&id, &other); err != nil {
			return err
		}
		if strings.EqualFold(other, name) {
			return fmt.Errorf("Name bereits vorhanden: %w", core.ErrValidation)
		}
	}
	return rows.Err()
}
