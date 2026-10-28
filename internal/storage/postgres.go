// Package storage owns the PostgreSQL connection, schema migrations, and the
// tenant-scoped transaction boundary that makes Row-Level Security bite.
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole is the unprivileged role every tenant transaction drops into. It is
// subject to RLS and holds no UPDATE/DELETE grants on the audit ledger.
const AppRole = "lde_app"

// SQLSTATEs raised by LabRat's own triggers (see migrations).
const (
	CodeLineageCycle  = "LDE01"
	CodeAuditLink     = "LDE02"
	CodeAppendOnly    = "LDE03"
	CodeSealed        = "LDE04"
	CodeRoleDenied    = "LDE05"
	codeUniqueViolate = "23505"
	codeFKViolation   = "23503"
)

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("storage: parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() { db.Pool.Close() }

// Principal is who a tenant transaction acts as: a principals.id, plus the
// human an agent acts for. The zero value is nobody: it can read, and every
// role check in the database fails.
type Principal struct {
	ID         string
	OnBehalfOf string
}

// WithTenant runs fn in a transaction confined to one tenant. It drops to
// AppRole and sets app.tenant_id, so every RLS policy applies to every
// statement fn issues. With no tenant set, policies match zero rows: the
// failure mode is "sees nothing", never "sees everything". It also sets
// app.principal_id and app.on_behalf_of from as, which the role checks read.
func (db *DB) WithTenant(ctx context.Context, tenantID string, as Principal, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+AppRole); err != nil {
			return fmt.Errorf("storage: assume %s: %w", AppRole, err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true),
			set_config('app.principal_id', $2, true), set_config('app.on_behalf_of', $3, true)`,
			tenantID, as.ID, as.OnBehalfOf); err != nil {
			return fmt.Errorf("storage: set tenant: %w", err)
		}
		return fn(tx)
	})
}

// WithOwner runs fn in a plain transaction as the connecting (owner) role,
// which RLS and role checks don't bind. It is for bootstrap and dev sign-in
// only; requests always go through WithTenant.
func (db *DB) WithOwner(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, db.Pool, pgx.TxOptions{}, fn)
}

// EnsureTenant creates a tenant if absent. It runs as the connecting
// (owner) role and is intended for bootstrap and dev seeding only.
func (db *DB) EnsureTenant(ctx context.Context, id, slug, name string) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`, id, slug, name)
	return err
}

// ErrorCode extracts the SQLSTATE from a Postgres error, or "".
func ErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsConflict reports constraint failures a client can fix by changing input.
func IsConflict(err error) bool {
	switch ErrorCode(err) {
	case CodeLineageCycle, CodeSealed, codeUniqueViolate:
		return true
	}
	return false
}

// IsBadReference reports a foreign key pointing at a row the tenant can't see.
func IsBadReference(err error) bool { return ErrorCode(err) == codeFKViolation }
