// Package config loads runtime configuration from the environment
// (twelve-factor). Every setting has a dev-friendly default so that
// `docker compose up -d && go run ./cmd/server` works with zero setup.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// DevTenantID is seeded when Env is "dev", so quickstart examples are copy-pasteable.
const DevTenantID = "00000000-0000-4000-8000-000000000001"

type Config struct {
	Env             string        // dev | prod
	HTTPAddr        string        // LDE_HTTP_ADDR
	DatabaseURL     string        // LDE_DATABASE_URL
	AutoMigrate     bool          // LDE_AUTO_MIGRATE
	LogLevel        slog.Level    // LDE_LOG_LEVEL
	ShutdownTimeout time.Duration // LDE_SHUTDOWN_TIMEOUT
	MaxIngestBytes  int64         // LDE_MAX_INGEST_BYTES, per streamed request

	// OIDC (SPEC-006). Issuer and audience are required in prod.
	OIDCIssuer      string        // LDE_OIDC_ISSUER
	OIDCAudience    string        // LDE_OIDC_AUDIENCE: the API's aud claim
	OIDCTenantClaim string        // LDE_OIDC_TENANT_CLAIM: claim naming the tenant uuid
	OIDCUIClientID  string        // LDE_OIDC_UI_CLIENT_ID: the UI's public client
	OIDCUIScopes    string        // LDE_OIDC_UI_SCOPES: space-separated
	UIIdleTimeout   time.Duration // LDE_UI_IDLE_TIMEOUT
}

func Load() (Config, error) {
	c := Config{
		Env:             env("LDE_ENV", "dev"),
		HTTPAddr:        env("LDE_HTTP_ADDR", ":8080"),
		DatabaseURL:     env("LDE_DATABASE_URL", "postgres://lde:lde@localhost:5432/lde?sslmode=disable"),
		OIDCIssuer:      env("LDE_OIDC_ISSUER", ""),
		OIDCAudience:    env("LDE_OIDC_AUDIENCE", ""),
		OIDCTenantClaim: env("LDE_OIDC_TENANT_CLAIM", "lde_tenant"),
		OIDCUIClientID:  env("LDE_OIDC_UI_CLIENT_ID", ""),
		OIDCUIScopes:    env("LDE_OIDC_UI_SCOPES", "openid profile email"),
	}
	var err error
	if c.AutoMigrate, err = strconv.ParseBool(env("LDE_AUTO_MIGRATE", "true")); err != nil {
		return c, fmt.Errorf("config: LDE_AUTO_MIGRATE: %w", err)
	}
	if err = c.LogLevel.UnmarshalText([]byte(env("LDE_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("config: LDE_LOG_LEVEL: %w", err)
	}
	if c.ShutdownTimeout, err = time.ParseDuration(env("LDE_SHUTDOWN_TIMEOUT", "15s")); err != nil {
		return c, fmt.Errorf("config: LDE_SHUTDOWN_TIMEOUT: %w", err)
	}
	if c.MaxIngestBytes, err = strconv.ParseInt(env("LDE_MAX_INGEST_BYTES", strconv.Itoa(512<<20)), 10, 64); err != nil {
		return c, fmt.Errorf("config: LDE_MAX_INGEST_BYTES: %w", err)
	}
	if c.UIIdleTimeout, err = time.ParseDuration(env("LDE_UI_IDLE_TIMEOUT", "15m")); err != nil || c.UIIdleTimeout < time.Minute {
		return c, fmt.Errorf("config: LDE_UI_IDLE_TIMEOUT must be a duration of at least 1m, got %q", os.Getenv("LDE_UI_IDLE_TIMEOUT"))
	}
	if c.Env != "dev" && c.Env != "prod" {
		return c, fmt.Errorf("config: LDE_ENV must be dev or prod, got %q", c.Env)
	}
	if c.Env == "prod" && (c.OIDCIssuer == "" || c.OIDCAudience == "") {
		return c, fmt.Errorf("config: LDE_ENV=prod needs LDE_OIDC_ISSUER and LDE_OIDC_AUDIENCE")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
