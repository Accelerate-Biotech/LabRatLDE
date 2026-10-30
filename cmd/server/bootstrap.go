package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/identity"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

var tenantRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// runBootstrapAdmin creates a tenant's first admin (SPEC-006-R12). It runs as
// the database owner, outside the role checks, and refuses once the tenant
// has an admin: after that, admins are granted through the API.
func runBootstrapAdmin(args []string) error {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant uuid (created if absent)")
	issuer := fs.String("issuer", "", "OIDC issuer of the admin's identity (the token's iss)")
	subject := fs.String("subject", "", "OIDC subject of the admin (the token's sub)")
	handle := fs.String("handle", "", "the admin's handle: lowercase letters, digits and ._@+-")
	displayName := fs.String("display-name", "", "the admin's name")
	email := fs.String("email", "", "the admin's email")
	slug := fs.String("slug", "", "slug for a new tenant (default t-<first 8 hex of its id>)")
	name := fs.String("name", "", "name for a new tenant (default the slug)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	*tenant = strings.ToLower(*tenant)
	switch {
	case fs.NArg() > 0:
		return fmt.Errorf("bootstrap-admin: unexpected argument %q", fs.Arg(0))
	case !tenantRE.MatchString(*tenant):
		return fmt.Errorf("bootstrap-admin: --tenant must be a uuid")
	case *issuer == "" || *subject == "":
		return fmt.Errorf("bootstrap-admin: --issuer and --subject are required")
	case !identity.ValidHandle(*handle):
		return fmt.Errorf("bootstrap-admin: --handle must be lowercase letters, digits and ._@+-")
	}
	if *slug == "" {
		*slug = "t-" + (*tenant)[:8]
	}
	if *name == "" {
		*name = *slug
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
	if cfg.AutoMigrate {
		if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
			return err
		}
	}

	const actor, reason = "system:bootstrap-admin", "bootstrap-admin"
	var id string
	err = db.WithOwner(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('lde.bootstrap:' || $1, 0))`, *tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
			*tenant, *slug, *name); err != nil {
			return fmt.Errorf("create tenant: %w", err)
		}
		var hasAdmin bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_grants
			WHERE tenant_id = $1 AND role = 'admin' AND revoked_at IS NULL)`, *tenant).Scan(&hasAdmin); err != nil {
			return err
		}
		if hasAdmin {
			return fmt.Errorf("tenant %s already has an admin; grant further admins through the API", *tenant)
		}
		p, err := identity.Create(ctx, tx, actor, reason, identity.Principal{
			TenantID: *tenant, Kind: "human", Handle: *handle, DisplayName: *displayName, Email: *email,
			Issuer: *issuer, Subject: *subject,
		})
		if err != nil {
			return fmt.Errorf("create principal: %w", err)
		}
		if _, err := identity.Grant(ctx, tx, actor, *tenant, p.ID, "admin", "", reason, p.ID); err != nil {
			return fmt.Errorf("grant admin: %w", err)
		}
		id = p.ID
		return nil
	})
	if err != nil {
		return fmt.Errorf("bootstrap-admin: %w", err)
	}
	fmt.Println(id)
	return nil
}
