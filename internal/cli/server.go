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
	"github.com/oniharnantyo/onclaw/internal/heartbeat"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/observability"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/scheduler"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
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

	// Langfuse trace export (integrate-langfuse-tracing D1): constructed only
	// when configured — the config-level gate decides, the handler constructor
	// is the absent capability. Set-but-invalid configuration fails fast here
	// (ValidateLangfuse names the offending ONCLAW_LANGFUSE_* variable).
	if err := cfg.ValidateLangfuse(); err != nil {
		stopWatchdog()
		return err
	}
	var traceHandler *observability.TraceHandler
	if cfg.LangfuseConfigured() {
		traceHandler, err = observability.NewLangfuseTraceHandler(observability.LangfuseConfig{
			Host:       cfg.LangfuseHost,
			PublicKey:  cfg.LangfusePublicKey,
			SecretKey:  cfg.LangfuseSecretKey,
			SampleRate: cfg.LangfuseSampleRate,
		})
		if err != nil {
			stopWatchdog()
			return fmt.Errorf("langfuse configuration invalid: %w", err)
		}
	}
	langfuseHost := ""
	if traceHandler != nil {
		langfuseHost = traceHandler.Host()
	}

	// Memory pipeline (integrate-agent-zero-memory 3.1/3.6/4.1): the worker,
	// the searcher, and the intent gate share the runner's side-call seam —
	// granular stores, the workspace provider catalog, and the instance
	// encryption key for per-workspace credential resolution. Side-calls are
	// Langfuse-traced only when the trace handler is configured (the same
	// `if traceHandler != nil` gate the runner's option uses). The chip sink
	// is late-bound to the runner (the channelRuntime.BindRunner pattern): the
	// worker is built first because the runner consumes it, and the chip
	// persist+broadcast live on the runner — the closure below reads `runner`
	// at chip time, after NewRunner has assigned it.
	memoryLog := slog.Default()
	var memoryTraceOpts []memory.SideCallOption
	if traceHandler != nil {
		memoryTraceOpts = append(memoryTraceOpts, memory.WithTraceCallback(traceHandler.Callback()))
	}
	var runner *agents.Runner
	// The embeddings lane (wave3-memory-vectors-and-graph D4): resolves the
	// workspace's embedding provider/model/dimension from the memory
	// settings record and the endpoint credential from the provider catalog
	// — the same stores the gister's side-call lane reads. The registry
	// resolves canonical origins for providers pinned without a base URL.
	memoryEmbedder := memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry())
	memoryWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memoryLog,
			append(memoryTraceOpts, memory.WithAgentModelSource(st.Agents()), memory.WithWorkspaceModelSource(st.ToolSettings()))...),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memoryLog,
			append(memoryTraceOpts, memory.WithAgentModelSource(st.Agents()), memory.WithWorkspaceModelSource(st.ToolSettings()))...),
		memoryEmbedder,
		st.MemoryEmbeddings(),
		memoryLog,
		memory.WithChipSink(func(ctx context.Context, job memory.IngestJob, payload memory.MemoryIngestedPayload) {
			runner.AppendMemoryChip(ctx, job, payload)
		}),
		// Workspace posture (integrate-agent-zero-memory D4): the per-workspace
		// visibility-default switch (narrow | org-shared) read from the memory
		// settings record; absence or read failure is the safe narrow default.
		memory.WithPostureFunc(func(ctx context.Context, workspaceID string) memory.Posture {
			return handlers.MemoryPostureForWorkspace(ctx, st.ToolSettings(), workspaceID)
		}),
		// Raw-embedding toggle (wave3): the per-workspace switch for the
		// raw-turn vector channel; absence or read failure is ON.
		memory.WithRawEmbeddingEnabled(func(ctx context.Context, workspaceID string) bool {
			return handlers.RawEmbeddingEnabledForWorkspace(ctx, st.ToolSettings(), workspaceID)
		}),
	)
	// The fused searcher (wave3 task 3.3): the runner's prefetch, the
	// memory.search tool, and the notes API free-text filter all read through
	// this one instance — lexical and vector channels fused with RRF over the
	// extracted stores, entity traversal over the graph, and raw-evidence
	// hydration through the session event log.
	memorySearcher := memory.NewSearcher(
		st.MemoryNotes(),
		st.MemoryEvents(),
		st.MemoryEmbeddings(),
		st.MemoryEntities(),
		st.SessionEvents(),
		memoryEmbedder,
	)
	intentGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memoryLog,
		append(memoryTraceOpts, memory.WithAgentModelSource(st.Agents()), memory.WithWorkspaceModelSource(st.ToolSettings()))...)

	// Memory consolidator (integrate-agent-zero-memory 6.1–6.3, D12): the
	// nightly per-workspace pass aligns to ~02:00 in each workspace's local
	// time and reads the worker's Stats counters for the morning report's
	// extraction-failure deltas. The consolidate-now endpoint reaches the same
	// code path through the router's narrow RunNow seam.
	memoryConsolidator := memory.NewConsolidator(
		st.MemoryNotes(),
		st.MemoryReports(),
		st.Workspaces(),
		st.MemoryEntities(),
		st.Providers(),
		encKey,
		agents.DefaultAgenticModelFactory,
		memoryWorker.Stats,
		memoryLog,
		memory.WithSideCall(append(memoryTraceOpts, memory.WithAgentModelSource(st.Agents()), memory.WithWorkspaceModelSource(st.ToolSettings()))...),
	)

	// Construct the runtime runner. ToolRegistry is built-in; NewDefaultToolRegistry
	// registers web.search. The runner handles session history queries and execution.
	// Run contexts derive from a process-lifetime base context, not the command
	// context: the command context is cancelled to trigger shutdown, which would
	// otherwise kill in-flight runs before they can drain.
	runnerOpts := []agents.RunnerOption{
		agents.WithBaseContext(context.Background()),
		agents.WithToolRegistry(agents.NewDefaultToolRegistry(
			st.Memories(),
			agents.WithSchedulerTools(st.Schedulers(), st.Channels()),
			agents.WithMemorySearch(memorySearcher),
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
		// The intent gate's per-workspace budget (fix-memory-retrieval-lane D3):
		// resolved from the workspace's memory settings record, falling back to
		// the default pin when absent.
		agents.WithMemoryGateBudget(memory.SettingsGateBudget(st.ToolSettings(), memoryLog)),
	}
	// The trace capability rides the callback chain only when configured (D1):
	// the rate must be the handler's own so the runner's persistence gate and
	// the upstream sampler agree (D5).
	if traceHandler != nil {
		runnerOpts = append(runnerOpts, agents.WithTraceHandler(traceHandler.Callback(), traceHandler.SampleRate()))
	}
	runner = agents.NewRunner(
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
		st.GatewayLinks(),
		memoryWorker,
		memorySearcher,
		intentGate,
		encKey,
		cfg.OnClawDir,
		runnerOpts...,
	)
	channelRuntime.BindRunner(runner)

	// Memory worker drain loop (integrate-agent-zero-memory 3.1): rides the
	// process-lifetime context like the scheduler and heartbeat loops — the
	// drain goroutines exit on shutdown cancel, and Stop() below waits for
	// in-flight jobs before the process exits.
	memoryWorker.Start(lifecycleCtx)

	// Consolidator loop (integrate-agent-zero-memory 6.1): the nightly
	// per-workspace pass rides the same process-lifetime context as the
	// scheduler and heartbeat loops — shutdown cancels the loop while Stop()
	// below waits for in-flight passes.
	memoryConsolidator.Start(lifecycleCtx)

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

	// Heartbeat loop (add-agent-heartbeat D14): the ticker claims due agent
	// heartbeats and fires them through the same runner the interactive
	// surfaces use — the runner's agent-level liveness is the busy guard
	// (D12) — with channel delivery through the chokepoint (D8) and
	// creator-DM delivery through the gateway stores. Its lifecycle rides the
	// same process-lifetime context as the scheduler loop: shutdown cancels
	// the ticker while Stop() waits for in-flight fires.
	heartbeatSvc := heartbeat.NewService(
		st.Heartbeats(),
		st.Users(),
		st.Agents(),
		st.Workspaces(),
		st.Channels(),
		st.Schedulers(),
		runner,
		runner,
		channelRuntime.Chokepoint(),
		st.Gateways(),
		st.GatewayLinks(),
		st.GatewayOutbox(),
		slog.Default(),
		heartbeat.WithTick(cfg.HeartbeatTick),
		heartbeat.WithRunTimeout(cfg.HeartbeatRunTimeout),
		heartbeat.WithConcurrency(cfg.HeartbeatConcurrency),
	)
	heartbeatSvc.Start(lifecycleCtx)

	// Gateway runtime (integrate-telegram-gateway D11): the gateway service,
	// pairing, adapter factory, and lifecycle manager assembled around the
	// same runner. StartAll brings every enabled workspace gateway up at boot
	// (one broken token is logged and skipped); the admin API calls Sync on
	// config changes; Stop tears the adapters down at shutdown. The WhatsApp
	// seams ride the same DSN (multi-device device store, add-whatsapp-gateway
	// design D6) and the configured Cloud API base override.
	gatewayRuntime := server.NewGatewayRuntime(
		st.Gateways(),
		st.GatewayBindings(),
		st.GatewayLinks(),
		st.GatewayOutbox(),
		st.SessionEvents(),
		st.Members(),
		st.Agents(),
		st.Users(),
		server.GatewayRunSubmitter(runner),
		st.Attachments(),
		wsResolver,
		encKey,
		cfg.DatabaseURL,
		cfg.WhatsAppCloudAPIBase,
	)
	bootWorkspaces, err := st.Workspaces().ListAll(ctx)
	if err != nil {
		stopWatchdog()
		return fmt.Errorf("failed to list workspaces for gateway startup: %w", err)
	}
	gatewayIDs := make([]string, 0, len(bootWorkspaces))
	for _, ws := range bootWorkspaces {
		gatewayIDs = append(gatewayIDs, ws.ID)
	}
	gatewayRuntime.Manager.StartAll(ctx, gatewayIDs)

	// Delivery outbox loop (integrate-telegram-gateway D9): redelivers
	// committed-but-unsent rows — the first cycle is the startup sweep — and
	// prunes delivered rows past retention. It rides the process-lifetime
	// context like the scheduler loop: stopWatchdog's cancel stops it during
	// shutdown, and undelivered rows survive for the next start.
	go gatewayRuntime.Outbox.Start(lifecycleCtx)

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
		Gateways:            gatewayRuntime,
		MemoryConsolidator:  memoryConsolidator,
		LangfuseHost:        langfuseHost,
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

	// Stop gateway ingestion before draining runs (design D11): with the HTTP
	// server down no new webhook updates or config mutations can arrive, and
	// stopping the adapters keeps long polling from minting new turns while
	// in-flight runs drain.
	gatewayRuntime.Manager.Stop(shutdownCtx)

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

	// Stop the heartbeat loop (add-agent-heartbeat D14) in the same bounded
	// pattern: the ticker halts and in-flight ticks keep their own
	// run-timeout-plus-drain-grace deadline beyond the window.
	slog.Info("stopping heartbeat loop", "window", cfg.RunDrainWindow)
	heartbeatDone := make(chan struct{})
	go func() {
		heartbeatSvc.Stop()
		close(heartbeatDone)
	}()
	select {
	case <-heartbeatDone:
	case <-time.After(cfg.RunDrainWindow):
		slog.Warn("heartbeat stop exceeded the drain window; in-flight ticks keep their own deadline")
	}

	// Stop the memory worker (integrate-agent-zero-memory 3.1): enqueue
	// acceptance halts (no runs are left to mint jobs) and in-flight jobs keep
	// the worker's own stop grace before the process exits. Queued-but-
	// unstarted jobs are abandoned — the raw session events stay intact for
	// the next start's reprocessing.
	slog.Info("stopping memory worker")
	memoryWorker.Stop()

	// Stop the memory consolidator (integrate-agent-zero-memory 6.1): the
	// nightly loop halts and in-flight passes keep the consolidator's own
	// stop grace before the process exits.
	slog.Info("stopping memory consolidator")
	memoryConsolidator.Stop()

	// Flush pending Langfuse exports (integrate-langfuse-tracing 3.3): after
	// the run drain and the scheduler stop every traced turn has reached its
	// terminal state, so one blocking flush sends whatever the batcher still
	// queues instead of dropping it at process exit. Best-effort by design
	// (D5) — the flush never fails startup or shutdown.
	if traceHandler != nil {
		traceHandler.Flush()
	}

	slog.Info("server exited cleanly")
	return nil
}
