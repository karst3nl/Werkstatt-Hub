package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

type migration struct {
	version              int
	name, body, checksum string
}

func (s *Store) migrate(ctx context.Context) error {
	paths, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)
	migrations := make([]migration, 0, len(paths))
	known := make(map[int]migration)
	for _, path := range paths {
		name := strings.TrimPrefix(path, "migrations/")
		ver, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil || ver < 1 {
			return fmt.Errorf("invalid migration filename %s", name)
		}
		if _, ok := known[ver]; ok {
			return fmt.Errorf("duplicate migration version %d", ver)
		}
		body, err := migrationFiles.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		m := migration{ver, name, string(body), hex.EncodeToString(sum[:])}
		migrations = append(migrations, m)
		known[ver] = m
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return s.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
   version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL
  )`)
		if err != nil {
			return err
		}
		applied := make(map[int]bool)
		rows, err := tx.QueryContext(ctx, "SELECT version,name,checksum FROM schema_migrations ORDER BY version")
		if err != nil {
			return err
		}
		for rows.Next() {
			var ver int
			var name, checksum string
			if err = rows.Scan(&ver, &name, &checksum); err != nil {
				rows.Close()
				return err
			}
			m, ok := known[ver]
			if !ok {
				rows.Close()
				return fmt.Errorf("database has unknown migration %d; use a newer application", ver)
			}
			if m.name != name || m.checksum != checksum {
				rows.Close()
				return fmt.Errorf("migration %d checksum or filename differs from database", ver)
			}
			applied[ver] = true
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		missing := false
		for _, m := range migrations {
			if applied[m.version] {
				if missing {
					return fmt.Errorf("migration history has a gap before %d", m.version)
				}
				continue
			}
			missing = true
			if _, err = tx.ExecContext(ctx, m.body); err != nil {
				return fmt.Errorf("%s: %w", m.name, err)
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,?,?,?)", m.version, m.name, m.checksum, timestamp(time.Now())); err != nil {
				return err
			}
		}
		return nil
	})
}
