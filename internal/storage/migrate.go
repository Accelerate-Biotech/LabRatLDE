package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLock is an arbitrary advisory-lock key so that several replicas
// booting at once apply migrations exactly once.
const migrationLock = 0x4C44455F4D494752 // "LDE_MIGR"

// MigrationVersions lists the embedded migration versions in apply order,
// e.g. "0001_core".
func MigrationVersions() ([]string, error) {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	versions := make([]string, len(names))
	for i, name := range names {
		versions[i] = strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
	}
	return versions, nil
}

// Migrate applies pending forward-only migrations, each in its own
// transaction. Migrations are embedded in the binary: the binary *is* the
// schema version. There are no down migrations; regulated systems roll forward.
func (db *DB) Migrate(ctx context.Context, log *slog.Logger) error {
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(migrationLock)); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(migrationLock)) //nolint:errcheck

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("migrate: bootstrap: %w", err)
	}

	versions, err := MigrationVersions()
	if err != nil {
		return err
	}

	for _, version := range versions {
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + version + ".sql")
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate %s: %w", version, err)
		}
		log.Info("applied migration", "version", version)
	}
	return nil
}
