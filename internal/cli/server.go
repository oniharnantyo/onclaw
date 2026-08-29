package cli

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

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

// serverCmd handles the "server" CLI command.
type serverCmd struct{}

// NewServerCmd creates a new serverCmd instance.
func NewServerCmd() *serverCmd {
	return &serverCmd{}
}

// Command returns the *cli.Command definition for "server".
func (s *serverCmd) Command() *cli.Command {
	return &cli.Command{
		Name:   "server",
		Usage:  "Start the OnClaw HTTP API server",
		Flags:  config.ServerFlags(),
		Action: s.Run,
	}
}

// Run starts the HTTP API server.
func (s *serverCmd) Run(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromServerContext(ctx, cmd)

	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	if cfg.JWTSecret == "" {
		slog.Warn("no JWT secret configured; using ephemeral secret (sessions will be invalidated on restart)")
	}

	st, err := store.Open(ctx, "postgres", store.DSNConfig{DSN: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("failed to open database store: %w", err)
	}
	defer st.Close()

	storageDriver := cfg.StorageDriver
	if storageDriver == "" {
		storageDriver = config.DefaultStorageDriver
	}
	stor, err := storage.Open(storageDriver, storage.StorageConfig{
		Driver:  storageDriver,
		DataDir: cfg.DataDir,
	})
	if err != nil {
		return fmt.Errorf("failed to open storage driver: %w", err)
	}

	bootstrapper := bootstrap.New(st)

	// Bootstrap master tenant
	master, err := bootstrapper.EnsureMaster(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure master tenant: %w", err)
	}
	slog.Info("master tenant verified", "slug", master.Slug, "id", master.ID)

	// Bootstrap initial superadmin from env if configured
	seedCfg := bootstrap.SuperadminSeedConfig{
		Email:        cfg.SuperadminEmail,
		Password:     cfg.SuperadminPassword,
		PasswordFile: cfg.SuperadminPasswordFile,
	}
	seededUser, err := bootstrapper.SeedSuperadmin(ctx, seedCfg)
	if err != nil {
		return fmt.Errorf("failed to seed superadmin: %w", err)
	}
	if seededUser != nil {
		slog.Info("superadmin seeded successfully", "email", seededUser.Email)
	}

	issuer := auth.NewJWTIssuer(auth.JWTConfig{
		Secret: cfg.JWTSecret,
		TTL:    cfg.TokenTTL,
	})

	router := server.NewRouter(server.RouterOptions{
		Store:   st,
		Storage: stor,
		Issuer:  issuer,
	})

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = config.DefaultListenAddr
	}

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}

	errChan := make(chan error, 1)
	go func() {
		slog.Info("listening for HTTP requests", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		slog.Info("received signal, shutting down server...", "signal", sig)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down server...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	slog.Info("server exited cleanly")
	return nil
}
