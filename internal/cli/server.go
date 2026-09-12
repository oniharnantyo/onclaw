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
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/scheduler"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
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

	// Workspace blob storage resolver (attachments design D15/D16): maps each
	// workspace's stored storage configuration to a driver instance — the
	// instance storage above is the default for unconfigured workspaces. One
	// instance is shared by the upload/serving API and the runner's drop-lane
	// materialization so the per-workspace driver cache is common.
	wsResolver := resolver.New(stor, st.WorkspaceStorage(), st.Attachments(), encKey, cfg.DataDir)

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

	// Channel fan-out (integrate-agent-channels D2/D11 + channel-teams D1/D3):
	// one runtime shared by the runner (channel context + feed + work
	// sessions) and the HTTP layer (post chokepoint + SSE hub). Built before
	// the runner because the runner's channel options consume the chokepoint;
	// the runtime late-binds the runner (BindRunner). The work-session store
	// backs the chokepoint's session branch (kickoff, hop accounting) and the
	// store-backed handles directory resolves roster @handles for mention
	// summons.
	channelRuntime := server.NewChannelRuntime(st.Channels(), st.WorkSessions(), st.Users(), st.Agents())

	// The stall watchdog (channel-teams D2) rides the process lifecycle
	// context, not the command context: the command context is cancelled to
	// trigger shutdown, but the watchdog must stop BEFORE the drain window
	// opens so it cannot mint new facilitator runs mid-shutdown. Its runs
	// derive from the runner's base context like every other background run.
	lifecycleCtx, stopWatchdog := context.WithCancel(context.Background())
	go channelRuntime.Chokepoint().StartWatchdog(lifecycleCtx)

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
		st.AgentSessions(),
		encKey,
		cfg.OnClawDir,
		agents.WithBaseContext(context.Background()),
		agents.WithToolRegistry(agents.NewDefaultToolRegistry(
			st.Memories(),
			agents.WithSchedulerTools(st.Schedulers(), st.Channels()),
		)),
		agents.WithToolPolicy(toolSettings),
		agents.WithMCPPolicy(mcp.NewSettingsPolicy(mcpSettings)),
		agents.WithMCPManager(mcpManager),
		agents.WithMCPStatusWriter(mcp.NewSettingsStatusWriter(mcpSettings)),
		agents.WithEnabledSkillReader(server.WorkspaceSkillReader(st.WorkspaceSkills())),
		agents.WithChannelContext(channelRuntime.Chokepoint()),
		agents.WithChannelFeed(channelRuntime.Chokepoint()),
		agents.WithWorkSessions(channelRuntime.Chokepoint()),
		agents.WithProjectSpace(channels.NewLocalProjectSpace(cfg.DataDir, st.Workspaces())),
		agents.WithAttachmentBlobs(wsResolver),
		agents.WithInputModalityResolver(modelCatalog),
	)
	channelRuntime.BindRunner(runner)

	// Scheduler loop (integrate-scheduler D3/D4): the ticker claims due
	// standing orders from the store and dispatches them through the same
	// runner the interactive surfaces use; channel delivery posts through the
	// chokepoint. Its lifecycle rides the process-lifetime context like the
	// stall watchdog — the command context is cancelled to trigger shutdown,
	// which stops the ticker, while Stop() waits for in-flight fires.
	schedulerSvc := scheduler.NewService(
		st.Schedulers(),
		st.Users(),
		st.Agents(),
		runner,
		channelRuntime.Chokepoint(),
		slog.Default(),
		scheduler.WithTick(cfg.SchedulerTick),
		scheduler.WithRunTimeout(cfg.SchedulerRunTimeout),
	)
	schedulerSvc.Start(lifecycleCtx)

	router := server.NewRouter(server.RouterOptions{
		Store:               st,
		Storage:             stor,
		Issuer:              issuer,
		EncryptionKey:       encKey,
		ModelCatalog:        modelCatalog,
		AgentService:        agentService,
		WorkspaceDir:        cfg.WorkspaceRoot(),
		OnClawDir:           cfg.OnClawDir,
		DataDir:             cfg.DataDir,
		Runner:              runner,
		ToolSettings:        toolSettings,
		MCPSettings:         mcpSettings,
		MCPManager:          mcpManager,
		ChannelRuntime:      channelRuntime,
		HooksCommandEnabled: cfg.HooksCommandEnabled,
		HooksScriptEnabled:  cfg.HooksScriptEnabled,
		WorkspaceStorage:    wsResolver,
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
		stopWatchdog()
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		slog.Info("received signal, shutting down server...", "signal", sig)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down server...")
	}

	// Stop the stall watchdog before the drain window opens: no new
	// facilitator summons may be minted while in-flight runs drain.
	stopWatchdog()

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

	// Stop the scheduler: the ticker halts (its loop also watches the
	// lifecycle context) and in-flight fires get the same drain window; the
	// service keeps its own longer deadline for stragglers beyond it.
	slog.Info("stopping scheduler loop", "window", cfg.RunDrainWindow)
	schedulerDone := make(chan struct{})
	go func() {
		schedulerSvc.Stop()
		close(schedulerDone)
	}()
	select {
	case <-schedulerDone:
	case <-time.After(cfg.RunDrainWindow):
		slog.Warn("scheduler stop exceeded the drain window; in-flight fires keep their own deadline")
	}

	slog.Info("server exited cleanly")
	return nil
}
