package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/agents/systemskills"
	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
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

	// OnClaw runtime paths derive from this root at every use; a relative
	// root would make them depend on the server's working directory.
	if !filepath.IsAbs(cfg.OnClawDir) {
		return fmt.Errorf("onclaw dir must be an absolute path (got %q; specify --onclaw-dir or ONCLAW_DIR)", cfg.OnClawDir)
	}

	encKey, err := config.ParseEncryptionKey(cfg.EncryptionKey)
	if err != nil {
		return err
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

	// Materialize the builtin instance hooks shipped with this binary (D15):
	// version-guarded upserts, then removal of builtin rows this binary no
	// longer ships. Idempotent and race-safe under concurrent starts.
	if err := bootstrap.SyncBuiltinHooks(ctx, st); err != nil {
		return fmt.Errorf("failed to sync builtin instance hooks: %w", err)
	}

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

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: cfg.JWTSecret,
		TTL:    cfg.TokenTTL,
	})

	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = config.DefaultCacheDir
	}
	modelCatalog := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir: cacheDir,
	})

	agentService := promptgen.NewService(st.Agents(), st.Providers(), encKey)
	if swept, err := agentService.Sweep(ctx); err == nil && swept > 0 {
		slog.Info("swept stale generating agents on startup", "count", swept)
	}

	// Mirror the embedded system skills into <ONClaw_DIR>/skills on every
	// start: changed files are overwritten, extraneous files removed — the
	// disk is a self-healing cache.
	if err := systemskills.SyncSystemSkills(domain.SystemSkillsDir(cfg.OnClawDir)); err != nil {
		return fmt.Errorf("sync system skills: %w", err)
	}

	// Workspace tool settings back the runtime's tool gate and the settings
	// API: the same service decrypts runtime configs and encrypts writes.
	toolSettings := agents.NewToolSettingsService(st.ToolSettings(), encKey)

	// MCP settings + connection manager (design.md D5/D10): the settings
	// service backs both the workspace/agent MCP API and the runtime policy;
	// the manager owns the lazy per-workspace connection cache. Its teardown
	// rides the composition root's lifecycle — connections outlive single
	// sessions deliberately.
	mcpSettings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), encKey)
	mcpManager := mcp.NewMCPManager()
	defer mcpManager.Close()

	// Construct the runtime runner. ToolRegistry is built-in; NewDefaultToolRegistry
	// registers web.search. The runner handles session history queries and execution.
	// Run contexts derive from a process-lifetime base context, not the command
	// context: the command context is cancelled to trigger shutdown, which would
	// otherwise kill in-flight runs before they can drain.
	runner := agents.NewRunner(
		st.Workspaces(),
		st.Agents(),
		st.Users(),
		st.Members(),
		st.Roles(),
		st.Providers(),
		st.SessionEvents(),
		st.SessionCheckpoints(),
		st.Memories(),
		encKey,
		cfg.OnClawDir,
		agents.WithBaseContext(context.Background()),
		agents.WithToolPolicy(toolSettings),
		agents.WithMCPPolicy(mcp.NewSettingsPolicy(mcpSettings)),
		agents.WithMCPManager(mcpManager),
		agents.WithMCPStatusWriter(mcp.NewSettingsStatusWriter(mcpSettings)),
		agents.WithEnabledSkillReader(server.WorkspaceSkillReader(st.WorkspaceSkills())),
	)

	router := server.NewRouter(server.RouterOptions{
		Store:               st,
		Storage:             stor,
		Issuer:              issuer,
		EncryptionKey:       encKey,
		ModelCatalog:        modelCatalog,
		AgentService:        agentService,
		WorkspaceDir:        cfg.WorkspaceRoot(),
		OnClawDir:           cfg.OnClawDir,
		Runner:              runner,
		ToolSettings:        toolSettings,
		MCPSettings:         mcpSettings,
		MCPManager:          mcpManager,
		HooksCommandEnabled: cfg.HooksCommandEnabled,
		HooksScriptEnabled:  cfg.HooksScriptEnabled,
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

	// The HTTP server is down, so no new runs can arrive. Give in-flight runs
	// the drain window to reach a terminal state; stragglers are cancelled and
	// record their cancel markers through the existing safe-point path.
	slog.Info("draining in-flight agent runs", "window", cfg.RunDrainWindow)
	runner.DrainRuns(cfg.RunDrainWindow)

	slog.Info("server exited cleanly")
	return nil
}
