// Command server is the LabRat LDE engine: one static binary that serves the
// API, applies its own schema migrations, and enforces the audit chain.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/api"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

// version is stamped at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "labrat:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting labrat", "version", version, "env", cfg.Env, "addr", cfg.HTTPAddr)

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	db, err := storage.Open(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()

	if cfg.AutoMigrate {
		if err := db.Migrate(ctx, log); err != nil {
			return err
		}
	}
	if cfg.Env == "dev" {
		if err := db.EnsureTenant(ctx, config.DevTenantID, "dev", "Development"); err != nil {
			return fmt.Errorf("seed dev tenant: %w", err)
		}
		log.Info("dev tenant ready", "tenant_id", config.DevTenantID)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(cfg, db, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No Read/WriteTimeout: instrument streams are long-lived by
		// design. Bound them at the proxy if you must.
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
