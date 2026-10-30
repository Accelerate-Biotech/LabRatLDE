package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

// Integrity triggers labrat iq requires. A spec that adds one adds it here.
var iqTriggers = []string{
	"audit_events_link", "audit_events_append_only", "audit_events_no_truncate",
	"audit_heads_append_only", "audit_heads_no_truncate",
	"lineage_edges_acyclic", "runs_signed_immutable",
	"lineage_edges_sealed", "lineage_nodes_sealed", "instrument_readings_sealed",
	"protocol_versions_append_only", "protocol_versions_no_truncate",
	"protocol_versions_gapless", "runs_protocol_pinned",
	"principals_immutable", "principals_no_truncate", "principals_admin_only",
	"principals_audited", "principals_enabled_audited",
	"role_grants_guard", "role_grants_no_truncate", "role_grants_audited",
	"agent_delegations_guard", "agent_delegations_no_truncate", "agent_delegations_audited",
	"experiments_role_gate", "protocol_versions_role_gate", "lineage_nodes_role_gate",
	"lineage_edges_role_gate", "instruments_role_gate", "instrument_readings_role_gate", "runs_role_gate",
	"change_sets_guard", "change_sets_no_truncate",
	"lineage_nodes_revisions", "runs_revisions", "experiments_revisions", "notes_revisions",
	"record_revisions_append_only", "record_revisions_no_truncate",
	"runs_params_sealed",
	"notes_guard", "notes_no_truncate", "notes_role_gate",
}

// runIQ is the installation qualification: read-only catalog checks that the
// deployed binary and database carry the integrity guarantees.
func runIQ(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("iq: unexpected argument %q", args[0])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	db, err := storage.Open(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()

	failed := 0
	report := func(ok bool, check, format string, a ...any) {
		status := "PASS"
		if !ok {
			status = "FAIL"
			failed++
		}
		fmt.Printf("%-4s  %-10s  %s\n", status, check, fmt.Sprintf(format, a...))
	}

	// binary
	if sum, err := executableSHA256(); err != nil {
		report(false, "binary", "cannot hash executable: %v", err)
	} else {
		report(true, "binary", "version %s  sha256 %s", version, sum)
	}

	// migrations
	embedded, err := storage.MigrationVersions()
	if err != nil {
		return err
	}
	rows, err := db.Pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("iq: migrations: %w", err)
	}
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("iq: migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iq: migrations: %w", err)
	}
	before := failed
	isEmbedded := map[string]bool{}
	for _, v := range embedded {
		isEmbedded[v] = true
		if !applied[v] {
			report(false, "migrations", "embedded migration %s is not applied", v)
		}
	}
	for v := range applied {
		if !isEmbedded[v] {
			report(false, "migrations", "applied migration %s is not embedded in this binary", v)
		}
	}
	if failed == before {
		report(true, "migrations", "%d embedded migrations applied", len(embedded))
	}

	// rls
	rows, err = db.Pool.Query(ctx, `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND (c.relname = 'tenants' OR EXISTS (
		        SELECT 1 FROM pg_attribute a
		        WHERE a.attrelid = c.oid AND a.attname = 'tenant_id'
		          AND a.attnum > 0 AND NOT a.attisdropped))
		ORDER BY c.relname`)
	if err != nil {
		return fmt.Errorf("iq: rls: %w", err)
	}
	before = failed
	tables := 0
	for rows.Next() {
		var name string
		var enabled, forced bool
		if err := rows.Scan(&name, &enabled, &forced); err != nil {
			rows.Close()
			return fmt.Errorf("iq: rls: %w", err)
		}
		tables++
		if !enabled {
			report(false, "rls", "%s: row level security is not enabled", name)
		}
		if !forced {
			report(false, "rls", "%s: row level security is not forced", name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iq: rls: %w", err)
	}
	if failed == before {
		report(true, "rls", "%d tenant-owned tables have RLS enabled and forced", tables)
	}

	// triggers
	rows, err = db.Pool.Query(ctx, `
		SELECT t.tgname, c.relname, t.tgenabled::text
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT t.tgisinternal AND n.nspname = 'public'`)
	if err != nil {
		return fmt.Errorf("iq: triggers: %w", err)
	}
	enabledByName := map[string]bool{}
	before = failed
	for rows.Next() {
		var name, table, state string
		if err := rows.Scan(&name, &table, &state); err != nil {
			rows.Close()
			return fmt.Errorf("iq: triggers: %w", err)
		}
		enabledByName[name] = enabledByName[name] || state != "D"
		if state == "D" {
			report(false, "triggers", "%s on %s is disabled", name, table)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iq: triggers: %w", err)
	}
	for _, name := range iqTriggers {
		if _, ok := enabledByName[name]; !ok {
			report(false, "triggers", "%s does not exist", name)
		}
	}
	if failed == before {
		report(true, "triggers", "%d integrity triggers exist and are enabled", len(iqTriggers))
	}

	// lde_app
	var super, bypass bool
	err = db.Pool.QueryRow(ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'lde_app'`).Scan(&super, &bypass)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		report(false, "lde_app", "role lde_app does not exist")
	case err != nil:
		return fmt.Errorf("iq: lde_app: %w", err)
	case super || bypass:
		report(false, "lde_app", "rolsuper=%t rolbypassrls=%t, want both false", super, bypass)
	default:
		report(true, "lde_app", "NOSUPERUSER NOBYPASSRLS")
	}

	if failed > 0 {
		return fmt.Errorf("iq: %d check(s) failed", failed)
	}
	return nil
}

func executableSHA256() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
