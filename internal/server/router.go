package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/heartbeat"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/scheduler"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/skills"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/storage/s3"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/teams"
)

// RouterOptions holds dependencies required by the HTTP API router.
type RouterOptions struct {
	Store         store.Store
	Storage       storage.Storage
	Issuer        services.TokenIssuer
	Auth          services.AuthService
	EncryptionKey []byte
	Providers     *providers.Registry
	ModelCatalog  *services.ModelCatalog
	AgentService  *promptgen.Service
	WorkspaceDir  string
	OnClawDir     string
	Runner        *agents.Runner
	ToolSettings  *agents.ToolSettingsService
	MCPSettings   *agents.MCPSettingsService
	MCPManager    *mcp.MCPManager
	// Connections is the workspace service-connections service (add-workspace-
	// connections 3.3), built by the composition root around the MCP settings
	// service so the token's secret row rides the one secret machinery. nil
	// builds a fresh one over the granular stores.
	Connections *services.ConnectionsService
	// MCPProbeTimeout bounds each MCP probe's fresh connection attempt; 0
	// uses the handler default (10s).
	MCPProbeTimeout time.Duration
	// V1StreamKeepAlive is the /v1 SSE idle keepalive cadence; 0 uses the
	// handler default (15s).
	V1StreamKeepAlive time.Duration
	// HooksCommandEnabled is the command hook handler kill switch (D10,
	// ONCLAW_HOOKS_COMMAND_ENABLED; default on). It drives the shared hook
	// registry's runtime gate and save-time validation alike.
	HooksCommandEnabled bool
	// HooksScriptEnabled is the script hook handler kill switch (D22,
	// ONCLAW_HOOKS_SCRIPT_ENABLED; default on). It drives the shared hook
	// registry's runtime gate.
	HooksScriptEnabled bool
	// ChannelRuntime is the channel fan-out assembly (integrate-agent-channels
	// D2/D11): the SSE hub and the single-post chokepoint, built by the
	// composition root around the runner so both share one instance. nil builds
	// a fresh one around the fallback runner.
	ChannelRuntime *ChannelRuntime
	// Scheduler is the scheduler ticker service (integrate-scheduler D3/D4),
	// built by the composition root around the runner (RunSubmitter) and the
	// channel chokepoint (ChannelPoster) so one runner serves every run
	// origin. nil builds a fresh one around the fallback runner; only the
	// composition root Start()s it, riding the server's lifecycle.
	Scheduler *scheduler.Service
	// Heartbeat is the heartbeat ticker service (add-agent-heartbeat D14),
	// built by the composition root around the same runner (RunSubmitter +
	// AgentBusyChecker) and the channel chokepoint (ChannelPoster). nil builds
	// a fresh one around the fallback runner; only the composition root
	// Start()s it, riding the server's lifecycle.
	Heartbeat *heartbeat.Service
	// MemoryConsolidator is the composition root's consolidator (integrate-
	// agent-zero-memory 6.1/6.2): when set, the memory handlers' consolidate-
	// now shares the same instance the nightly ticker drives — one code path
	// with the worker's extraction-failure counters. Nil (fallback assembly)
	// builds a ticker-less consolidator with an unwired stats source.
	MemoryConsolidator *memory.Consolidator

	// Gateways is the gateway runtime (integrate-telegram-gateway D11, task
	// 6.2): the gateway service + lifecycle manager + pairing service built
	// by the composition root around the runner. nil builds a fresh one for
	// test assembly; the composition root drives Manager.StartAll/Stop on the
	// server lifecycle, and the handlers call Manager.Sync after config
	// changes.
	Gateways *GatewayRuntime
	// DatabaseURL is the PostgreSQL DSN the multi-device WhatsApp lane
	// bridges whatsmeow's sqlstore over (add-whatsapp-gateway design D6) —
	// the same DSN the store uses. Empty keeps the md lane unconstructable
	// (its adapter factory fails loudly; no other behavior changes).
	DatabaseURL string
	// WhatsAppCloudAPIBase overrides the Cloud API endpoint for the WhatsApp
	// cloud lane (add-whatsapp-gateway design D10; ONCLAW_WHATSAPP_CLOUD_API_BASE).
	// Empty keeps the public graph.facebook.com endpoint.
	WhatsAppCloudAPIBase string
	// LangfuseHost is the configured Langfuse backend the scheduler-run views
	// compose their deep links from (integrate-langfuse-tracing D6). Empty —
	// tracing unconfigured — keeps every run payload's langfuse_url null.
	LangfuseHost string
	// ChannelStreamKeepAlive is the channel SSE idle keepalive cadence; 0 uses
	// the handler default (15s).
	ChannelStreamKeepAlive time.Duration
	// DataDir is the storage driver's data directory; the channel project
	// space (channel-teams D5, projects/<slug> mounted at /project) roots
	// under it. Empty falls back to ./data relative to the process.
	DataDir string
	// WorkspaceStorage resolves per-workspace blob storage for chat
	// attachments (attachments design D15/D16): uploads resolve the
	// configured backend, capability serving streams from the backend
	// recorded on the attachment row, and drop-lane runs materialize through
	// it. nil builds a fresh resolver around the instance storage.
	WorkspaceStorage *resolver.WorkspaceStorage
}

// ---------------------------------------------------------------------------
// Channel runtime (design integrate-agent-channels D2/D11)
// ---------------------------------------------------------------------------

// ChannelRuntime bundles the channel fan-out assembly the composition root
// shares between the runner and the HTTP layer: the hub fans feed events out
// to SSE subscribers; the chokepoint is the single message pipeline that
// persists, resolves mentions, and mints agent runs.
//
// The runner and the chokepoint reference each other — the runner's channel
// context and feed ARE the chokepoint (agents.WithChannelContext /
// WithChannelFeed), while the chokepoint's fan-out submits runs back through
// the runner — so the RunSubmitter side is late-bound: build the runtime,
// construct the runner with Chokepoint(), then BindRunner.
type ChannelRuntime struct {
	hub        *channels.Hub
	chokepoint *channels.Chokepoint
	binding    *lateBoundRunner
}

// lateBoundRunner breaks the runner→chokepoint→runner construction cycle by
// holding the runner's Run until BindRunner installs it.
type lateBoundRunner struct {
	mu  sync.RWMutex
	run func(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
}

// Run implements the chokepoint's RunSubmitter dependency.
func (b *lateBoundRunner) Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	b.mu.RLock()
	run := b.run
	b.mu.RUnlock()
	if run == nil {
		return nil, fmt.Errorf("channel fan-out reached the runner before BindRunner (composition root wiring bug)")
	}
	return run(ctx, req)
}

// NewChannelRuntime builds the channel hub and chokepoint around a not-yet-
// constructed runner. The work-session store (channel-teams D1/D3) backs the
// chokepoint's session branch — kickoff, hop accounting, and the watchdog —
// and the store-backed handles directory resolves roster @handles for
// mention summons.
func NewChannelRuntime(channelStore store.ChannelStore, sessions store.WorkSessionStore, users store.UserStore, agents store.AgentStore) *ChannelRuntime {
	hub := channels.NewHub()
	binding := &lateBoundRunner{}
	return &ChannelRuntime{
		hub:     hub,
		binding: binding,
		chokepoint: channels.NewChokepoint(channelStore, binding, hub,
			channels.WithWorkSessionStore(sessions),
			channels.WithChannelHandles(newStoreChannelHandles(users, agents)),
		),
	}
}

// Hub returns the SSE fan-out hub.
func (cr *ChannelRuntime) Hub() *channels.Hub { return cr.hub }

// Chokepoint returns the single message pipeline (D2). It satisfies the
// runner's ChannelContext and ChannelFeed interfaces.
func (cr *ChannelRuntime) Chokepoint() *channels.Chokepoint { return cr.chokepoint }

// BindRunner installs the runner's Run as the chokepoint's fan-out submitter;
// *agents.Runner satisfies channels.RunSubmitter (pinned contract). Called
// exactly once by the composition root after the runner is constructed.
func (cr *ChannelRuntime) BindRunner(runner *agents.Runner) {
	cr.binding.mu.Lock()
	cr.binding.run = runner.Run
	cr.binding.mu.Unlock()
}

// router configures and builds the HTTP API routes and handlers.
type router struct {
	opts RouterOptions
	mw   *middlewares
	v1mw *v1Middlewares
}

// New creates a new router instance with all handlers and middlewares initialized.
func New(opts RouterOptions) *router {
	if opts.Store == nil {
		return &router{opts: opts}
	}
	return &router{
		opts: opts,
		mw: NewMiddlewares(
			opts.Store.Users(),
			opts.Store.Workspaces(),
			opts.Store.Members(),
			opts.Store.Roles(),
			opts.Issuer,
		),
		v1mw: NewV1Middlewares(opts.Store.APIKeys()),
	}
}

// Engine builds and returns the configured Gin Engine.
func (rt *router) Engine() *gin.Engine {
	authService := rt.opts.Auth
	if authService == nil && rt.opts.Store != nil && rt.opts.Issuer != nil {
		reg := services.NewRegistry()
		reg.Register(services.NewPasswordProvider(rt.opts.Store.Users()))
		authService = services.NewAuthService(rt.opts.Store.Users(), rt.opts.Store.Members(), rt.opts.Issuer, reg)
	}

	providerRegistry := rt.opts.Providers
	if providerRegistry == nil {
		providerRegistry = providers.NewRegistry()
	}

	modelCatalog := rt.opts.ModelCatalog
	if modelCatalog == nil {
		modelCatalog = services.NewModelCatalog(services.ModelCatalogOptions{Registry: providerRegistry})
	}

	agentService := rt.opts.AgentService
	if agentService == nil && rt.opts.Store != nil {
		agentService = promptgen.NewService(rt.opts.Store.Agents(), rt.opts.Store.Providers(), rt.opts.EncryptionKey)
	}

	workspaceDir := rt.opts.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = domain.WorkspaceRoot(domain.DefaultOnClawDir())
	}
	onClawDir := rt.opts.OnClawDir
	if onClawDir == "" {
		onClawDir = domain.DefaultOnClawDir()
	}

	// Project space root (channel-teams D5): projects/<channel-slug> under the
	// storage data dir, mounted read-write at /project into member agents'
	// jails through the runner option below.
	dataDir := rt.opts.DataDir
	if dataDir == "" {
		dataDir = config.DefaultDataDir
	}

	// Workspace blob storage resolver (attachments design D15/D16): maps each
	// workspace's stored configuration to a driver instance — the injected
	// one when the composition root already built it (sharing its cache with
	// the runner's drop-lane resolution), a fresh one over the instance
	// storage and stores otherwise (test-assembly fallback).
	wsStorage := rt.opts.WorkspaceStorage
	if wsStorage == nil {
		wsStorage = resolver.New(rt.opts.Storage, rt.opts.Store.WorkspaceStorage(), rt.opts.Store.Attachments(), rt.opts.EncryptionKey, dataDir)
	}

	authHandlers := handlers.NewAuthHandlers(authService, rt.opts.Storage)
	workspaceHandlers := handlers.NewWorkspaceHandlers(rt.opts.Store, rt.opts.EncryptionKey, providerRegistry, modelCatalog, agentService, workspaceDir)
	memberHandlers := handlers.NewMemberHandlers(rt.opts.Store, rt.opts.Storage)
	roleHandlers := handlers.NewRoleHandlers(rt.opts.Store.Roles())
	userHandlers := handlers.NewUserHandlers(rt.opts.Store.Users(), rt.opts.Storage)
	fileHandlers := handlers.NewFileHandlers(rt.opts.Storage, rt.opts.Store.Attachments(), wsStorage)
	attachmentHandlers := handlers.NewAttachmentsHandlers(rt.opts.Store.Attachments(), wsStorage)
	storageConfigHandlers := handlers.NewStorageConfigHandlers(rt.opts.Store.WorkspaceStorage(), rt.opts.EncryptionKey, s3.Probe)
	adminWorkspaceHandlers := handlers.NewAdminWorkspaceHandlers(rt.opts.Store, rt.opts.Storage)
	adminUserHandlers := handlers.NewAdminUserHandlers(rt.opts.Store.Users(), rt.opts.Store.Workspaces(), rt.opts.Store.Members(), rt.opts.Store.Roles())
	adminSuperadminHandlers := handlers.NewAdminSuperadminHandlers(rt.opts.Store)
	// MCP settings + connection manager: the service backs both the settings
	// API and the runtime policy; the manager owns the lazy connection cache.
	// The composition root may inject long-lived instances (whose Close rides
	// its lifecycle); the fallback builds fresh ones.
	mcpSettings := rt.opts.MCPSettings
	if mcpSettings == nil {
		mcpSettings = agents.NewMCPSettingsService(rt.opts.Store.WorkspaceMCPServers(), rt.opts.Store.AgentMCPServers(), rt.opts.Store.Agents(), rt.opts.EncryptionKey)
	}
	mcpManager := rt.opts.MCPManager
	if mcpManager == nil {
		mcpManager = mcp.NewMCPManager()
	}

	// Shared hook handler registry (D9/D10): one instance serves the REST
	// dry-run endpoint AND the runtime dispatcher. The command kill switch
	// rides the composition root's flag; the mcp_tool handler invokes
	// workspace-level servers through the shared manager; the prompt handler
	// resolves evaluator models through the workspace provider catalog.
	hookRegistry := agenthooks.NewRegistry(
		agenthooks.WithEncryptionKey(rt.opts.EncryptionKey),
		agenthooks.WithCommandEnabled(rt.opts.HooksCommandEnabled),
		agenthooks.WithScriptEnabled(rt.opts.HooksScriptEnabled),
		agenthooks.WithMCPInvoker(mcp.NewHooksInvoker(mcpSettings, mcpManager)),
		agenthooks.WithEvaluatorFactory(agents.NewHookEvaluatorFactory(rt.opts.Store.Providers(), rt.opts.EncryptionKey, agents.DefaultAgenticModelFactory)),
	)

	// Channel fan-out (integrate-agent-channels D2/D11): one runtime shared by
	// the runner (channel context + feed) and the HTTP layer (post chokepoint +
	// SSE hub). Built before the runner because the runner's channel options
	// consume the chokepoint. The work-session store (channel-teams D1) backs
	// the chokepoint's session branch.
	channelRuntime := rt.opts.ChannelRuntime
	if channelRuntime == nil && rt.opts.Store != nil {
		channelRuntime = NewChannelRuntime(rt.opts.Store.Channels(), rt.opts.Store.WorkSessions(), rt.opts.Store.Users(), rt.opts.Store.Agents())
	}

	// Project space root (channel-teams D5) — dataDir computed above with the
	// workspace storage resolver.

	// Memory pipeline (integrate-agent-zero-memory): the fallback assembly
	// mirrors the composition root's construction — worker, searcher, and
	// intent gate over the same stores, with the chip sink late-bound to the
	// runner built below. The worker is Started by the composition root; the
	// fallback path never drains, like the fallback scheduler/heartbeat.
	// The embeddings lane and the fused searcher are built unconditionally:
	// the searcher serves the memory-notes handlers' free-text filter on the
	// injected-runner path too (wave3 task 3.3 — one fused read path).
	memoryLog := slog.Default()
	memoryEmbedder := memory.NewProviderEmbedder(rt.opts.Store.Providers(), rt.opts.Store.ToolSettings(), rt.opts.EncryptionKey, providerRegistry)
	memorySearcher := memory.NewSearcher(
		rt.opts.Store.MemoryNotes(),
		rt.opts.Store.MemoryEvents(),
		rt.opts.Store.MemoryEmbeddings(),
		rt.opts.Store.MemoryEntities(),
		rt.opts.Store.SessionEvents(),
		memoryEmbedder,
	)

	runner := rt.opts.Runner
	if runner == nil && rt.opts.Store != nil {
		memoryWorker := memory.NewWorker(
			memory.NewGister(rt.opts.Store.MemoryEvents(), rt.opts.Store.SessionEvents(), rt.opts.Store.MemoryEntities(), rt.opts.Store.Providers(), rt.opts.EncryptionKey, agents.DefaultAgenticModelFactory, memoryLog),
			memory.NewGate(rt.opts.Store.MemoryNotes(), rt.opts.Store.MemoryEntities(), rt.opts.Store.Memories(), rt.opts.Store.Providers(), rt.opts.EncryptionKey, agents.DefaultAgenticModelFactory, memoryLog),
			memoryEmbedder,
			rt.opts.Store.MemoryEmbeddings(),
			memoryLog,
			memory.WithChipSink(func(ctx context.Context, job memory.IngestJob, payload memory.MemoryIngestedPayload) {
				runner.AppendMemoryChip(ctx, job, payload)
			}),
			// Workspace visibility posture (D4's policy switch): read off the
			// memory settings record per job, failing safe to narrow.
			memory.WithPostureFunc(func(ctx context.Context, workspaceID string) memory.Posture {
				return handlers.MemoryPostureForWorkspace(ctx, rt.opts.Store.ToolSettings(), workspaceID)
			}),
			// Raw-embedding toggle (wave3): absence or read failure is ON.
			memory.WithRawEmbeddingEnabled(func(ctx context.Context, workspaceID string) bool {
				return handlers.RawEmbeddingEnabledForWorkspace(ctx, rt.opts.Store.ToolSettings(), workspaceID)
			}),
		)
		intentGate := memory.NewIntentGate(rt.opts.Store.Providers(), rt.opts.EncryptionKey, agents.DefaultAgenticModelFactory, memoryLog)

		runner = agents.NewRunner(
			rt.opts.Store.Workspaces(),
			rt.opts.Store.Agents(),
			rt.opts.Store.Users(),
			rt.opts.Store.Members(),
			rt.opts.Store.Roles(),
			rt.opts.Store.Providers(),
			rt.opts.Store.SessionEvents(),
			rt.opts.Store.SessionCheckpoints(),
			rt.opts.Store.Memories(),
			rt.opts.Store.AgentSessions(),
			rt.opts.Store.GatewayLinks(),
			memoryWorker,
			memorySearcher,
			intentGate,
			rt.opts.EncryptionKey,
			onClawDir,
			agents.WithToolRegistry(agents.NewDefaultToolRegistry(
				rt.opts.Store.Memories(),
				agents.WithSchedulerTools(rt.opts.Store.Schedulers(), rt.opts.Store.Channels()),
				agents.WithMemorySearch(memorySearcher),
				agents.WithTodoTools(rt.opts.Store.Todos()),
			)),
			agents.WithEnabledSkillReader(WorkspaceSkillReader(rt.opts.Store.WorkspaceSkills())),
			agents.WithMCPPolicy(mcp.NewSettingsPolicy(mcpSettings)),
			agents.WithMCPManager(mcpManager),
			agents.WithMCPStatusWriter(mcp.NewSettingsStatusWriter(mcpSettings)),
			agents.WithHooks(agenthooks.NewDispatcher(rt.opts.Store.Hooks(), hookRegistry)),
			agents.WithChannelContext(channelRuntime.Chokepoint()),
			agents.WithChannelFeed(channelRuntime.Chokepoint()),
			agents.WithWorkSessions(channelRuntime.Chokepoint()),
			agents.WithProjectSpace(channels.NewLocalProjectSpace(dataDir, rt.opts.Store.Workspaces())),
			agents.WithAttachmentBlobs(wsStorage),
			agents.WithInputModalityResolver(modelCatalog),
			// The todo store (adopt-assistant-ui-elements D6): the open-items
			// summary the runner composes per turn for agents exposing the tools.
			agents.WithTodoStore(rt.opts.Store.Todos()),
			// Gate budget (fix-memory-retrieval-lane D3): read off the
			// memory settings record per turn, absence resolving to the
			// default.
			agents.WithMemoryGateBudget(memory.SettingsGateBudget(rt.opts.Store.ToolSettings(), memoryLog)),
		)
		channelRuntime.BindRunner(runner)
	}

	// Scheduler loop (integrate-scheduler D3/D4): one service shared by the
	// ticker (started by the composition root on its lifecycle context) and
	// the HTTP run-now endpoint, dispatched through the same runner and
	// posting channel delivery through the same chokepoint.
	schedulerSvc := rt.opts.Scheduler
	if schedulerSvc == nil && rt.opts.Store != nil {
		schedulerSvc = scheduler.NewService(
			rt.opts.Store.Schedulers(),
			rt.opts.Store.Users(),
			rt.opts.Store.Agents(),
			runner,
			channelRuntime.Chokepoint(),
			slog.Default(),
		)
	}
	schedulerHandlers := handlers.NewSchedulerHandlers(rt.opts.Store.Schedulers(), schedulerSvc, rt.opts.LangfuseHost)

	// Heartbeat loop (add-agent-heartbeat D14): one service shared by the
	// ticker (started by the composition root on its lifecycle context) and
	// the run-now/resume endpoints, dispatched through the same runner — whose
	// agent-level liveness is the busy guard (D12) — with channel delivery
	// through the same chokepoint and creator-DM delivery through the gateway
	// stores (D8). The channels + schedulers stores feed the activity digest
	// composer (D9).
	heartbeatSvc := rt.opts.Heartbeat
	if heartbeatSvc == nil && rt.opts.Store != nil {
		heartbeatSvc = heartbeat.NewService(
			rt.opts.Store.Heartbeats(),
			rt.opts.Store.Users(),
			rt.opts.Store.Agents(),
			rt.opts.Store.Workspaces(),
			rt.opts.Store.Channels(),
			rt.opts.Store.Schedulers(),
			runner,
			runner,
			channelRuntime.Chokepoint(),
			rt.opts.Store.Gateways(),
			rt.opts.Store.GatewayLinks(),
			rt.opts.Store.GatewayOutbox(),
			slog.Default(),
		)
	}
	heartbeatHandlers := handlers.NewAgentHeartbeatHandlers(rt.opts.Store.Agents(), rt.opts.Store.Heartbeats(), rt.opts.Store.Workspaces(), heartbeatSvc)

	// Gateway runtime (integrate-telegram-gateway D11, task 6.2): the
	// composition root builds it around the same runner; the fallback assembles
	// a fresh one for tests. Handlers reconcile the manager after config
	// mutations; the composition root owns StartAll/Stop.
	gatewayRuntime := rt.opts.Gateways
	if gatewayRuntime == nil && rt.opts.Store != nil {
		gatewayRuntime = NewGatewayRuntime(
			rt.opts.Store.Gateways(),
			rt.opts.Store.GatewayBindings(),
			rt.opts.Store.GatewayLinks(),
			rt.opts.Store.GatewayOutbox(),
			rt.opts.Store.SessionEvents(),
			rt.opts.Store.Members(),
			rt.opts.Store.Agents(),
			rt.opts.Store.Users(),
			GatewayRunSubmitter(runner),
			rt.opts.Store.Attachments(),
			wsStorage,
			rt.opts.EncryptionKey,
			rt.opts.DatabaseURL,
			rt.opts.WhatsAppCloudAPIBase,
		)
	}
	gatewayHandlers := handlers.NewGatewayHandlers(
		rt.opts.Store.Gateways(),
		rt.opts.Store.GatewayBindings(),
		rt.opts.Store.GatewayLinks(),
		rt.opts.Store.GatewayOutbox(),
		rt.opts.Store.Agents(),
		rt.opts.Store.Users(),
		gatewayRuntime.Pairing,
		gatewayRuntime.Manager,
		gatewayRuntime.Service,
		gatewayRuntime.Verifier,
		rt.opts.EncryptionKey,
		gatewayRuntime,
		gatewayRuntime,
	)

	providerHandlers := handlers.NewProviderHandlers(rt.opts.Store.Providers(), rt.opts.Store.Agents(), rt.opts.EncryptionKey, providerRegistry, modelCatalog)
	agentHandlers := handlers.NewAgentHandlers(rt.opts.Store.Agents(), rt.opts.Store.Providers(), rt.opts.Store.SessionEvents(), rt.opts.Store.AgentSessions(), rt.opts.EncryptionKey, providerRegistry, modelCatalog, agentService, workspaceDir, runner, runner)
	// Agent workspace files (add-right-panel 2.1): read-only byte reads and
	// one-level listings over the agent jail directory; the handler owns the
	// path confinement and content-type serving guards.
	workspaceFilesHandlers := handlers.NewWorkspaceFilesHandlers(rt.opts.Store.Agents(), workspaceDir)
	memoryHandlers := handlers.NewMemoryHandlers(rt.opts.Store.Memories())

	// Memory notes/events surface (integrate-agent-zero-memory tasks 5.4):
	// the extracted-facts browser, episodic timeline, consolidate-now, the
	// morning report, and the workspace memory settings record. The fallback
	// assembly builds the same consolidator the composition root's nightly
	// ticker drives (loop never Started here — like the fallback scheduler,
	// only the composition root runs tickers); the handlers only ever call
	// RunNow through the narrow seam, so one code path serves both triggers
	// (tasks 6.2).
	memoryConsolidator := rt.opts.MemoryConsolidator
	if memoryConsolidator == nil {
		memoryConsolidator = memory.NewConsolidator(
			rt.opts.Store.MemoryNotes(),
			rt.opts.Store.MemoryReports(),
			rt.opts.Store.Workspaces(),
			rt.opts.Store.MemoryEntities(),
			rt.opts.Store.Providers(),
			rt.opts.EncryptionKey,
			agents.DefaultAgenticModelFactory,
			nil, // stats: the fallback has no worker to read counters from
			memoryLog,
		)
	}
	memoryNoteHandlers := handlers.NewMemoryNoteHandlers(
		rt.opts.Store.MemoryNotes(),
		rt.opts.Store.MemoryEvents(),
		memorySearcher,
		rt.opts.Store.MemoryReports(),
		rt.opts.Store.ToolSettings(),
		rt.opts.Store.Providers(),
		memoryConsolidator,
		rt.opts.EncryptionKey,
		providerRegistry,
	)

	toolSettings := rt.opts.ToolSettings
	if toolSettings == nil {
		toolSettings = agents.NewToolSettingsService(rt.opts.Store.ToolSettings(), rt.opts.EncryptionKey)
	}
	toolSettingsHandlers := handlers.NewToolSettingsHandlers(toolSettings)

	// Workspace service connections (add-workspace-connections 3.3): the
	// connections service composes the stores with the MCP settings service —
	// the token's secret row rides the same machinery, no second secret path
	// (design.md D4). The composition root may inject a long-lived instance;
	// the fallback builds a fresh one over the granular stores.
	connectionsSvc := rt.opts.Connections
	if connectionsSvc == nil {
		connectionsSvc = services.NewConnectionsService(
			rt.opts.Store.Connections(),
			rt.opts.Store.WorkspaceMCPServers(),
			rt.opts.Store.Agents(),
			mcpSettings,
			services.WithProbeTimeout(rt.opts.MCPProbeTimeout),
		)
	}
	mcpServerHandlers := handlers.NewMCPServerHandlers(mcpSettings, rt.opts.Store.Agents(), mcpManager, rt.opts.MCPProbeTimeout, connectionsSvc)
	connectionsHandlers := handlers.NewConnectionsHandlers(connectionsSvc, mcpManager)

	// Channel surface (integrate-agent-channels D11/D12 + channel-teams
	// tasks 6/7): posts and kickoffs ride the shared chokepoint; the SSE
	// endpoint subscribes the shared hub; work-session reads ride the store;
	// template materialization spawns through the shared agent creation path.
	spawner := handlers.NewTeamsAgentSpawner(
		handlers.AgentCreationDeps{
			Agents:       rt.opts.Store.Agents(),
			Providers:    rt.opts.Store.Providers(),
			Registry:     providerRegistry,
			ModelCatalog: modelCatalog,
			AgentService: agentService,
			WorkspaceDir: workspaceDir,
		},
		rt.opts.Store.Providers(),
		rt.opts.Store.Workspaces(),
	)
	channelHandlers := handlers.NewChannelHandlers(
		rt.opts.Store.Channels(),
		rt.opts.Store.Agents(),
		rt.opts.Store.Users(),
		channelRuntime.Chokepoint(),
		channelRuntime.Chokepoint(),
		channelRuntime.Chokepoint(),
		rt.opts.Store.WorkSessions(),
		teams.NewMaterializer(rt.opts.Store.Channels(), rt.opts.Store.Agents(), spawner),
		channelRuntime.Hub(),
		rt.opts.ChannelStreamKeepAlive,
	)

	// Save-time match counts (D8) enumerate the workspace-visible toolset:
	// the built-in tool surface (a fresh default registry's names — identical
	// to the runner's; no tool is constructed) plus the workspace's enabled
	// MCP servers' tools through the shared manager.
	hookToolValues := handlers.NewWorkspaceHookToolValueSource(
		agents.NewDefaultToolRegistry(
			rt.opts.Store.Memories(),
			agents.WithSchedulerTools(rt.opts.Store.Schedulers(), rt.opts.Store.Channels()),
			agents.WithTodoTools(rt.opts.Store.Todos()),
		),
		rt.opts.Store.WorkspaceMCPServers(),
		mcpManager,
	)
	hookHandlers := handlers.NewHookHandlers(rt.opts.Store.Hooks(), rt.opts.Store.Agents(), rt.opts.Store.WorkspaceMCPServers(), hookRegistry, hookToolValues, rt.opts.EncryptionKey, rt.opts.HooksCommandEnabled)

	// Workspace skill library: the registry store bridges into the install
	// pipeline's port (skills.Store), the transaction seam binds to
	// store.WithTx, and the runner reads enabled names through the same
	// adapter (design D2–D5, D8).
	skillsAdapter := newWorkspaceSkillStoreAdapter(rt.opts.Store.WorkspaceSkills())
	installService := skills.NewInstallService(
		skillsAdapter,
		rt.opts.Store.Agents(),
		rt.opts.Store.ToolSettings(),
		skills.WithTxProvider(workspaceSkillsTxProvider(rt.opts.Store)),
	)
	skillHandlers := handlers.NewSkillHandlers(installService, rt.opts.Store.WorkspaceSkills(), rt.opts.Store.Agents(), onClawDir)

	apiKeyService := services.NewAPIKeyService(rt.opts.Store.APIKeys())
	apiKeyHandlers := handlers.NewAPIKeysHandlers(apiKeyService)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// /v1 (OpenResponses) surface: API-key authenticated only (JWTs rejected).
	v1Handlers := handlers.NewV1Handlers(runner, rt.opts.Store.Agents(), rt.opts.Store.SessionEvents(), rt.opts.Store.Attachments(), wsStorage, rt.opts.V1StreamKeepAlive)
	v1 := r.Group("/v1")
	v1.Use(rt.v1mw.APIKeyAuthRequired())
	{
		v1.GET("/models", v1Handlers.ListModels)
		v1.POST("/responses", v1Handlers.CreateResponse)
	}

	// Basic health check
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Public capability file serving routes
	r.GET("/files/:key", fileHandlers.ServeFile)
	r.GET("/files/avatars/:name", fileHandlers.ServeFile)

	api := r.Group("/api/v1")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})

		// Public capability file serving under /api/v1
		api.GET("/files/:key", fileHandlers.ServeFile)
		api.GET("/files/avatars/:name", fileHandlers.ServeFile)

		// Public auth endpoints
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", authHandlers.Login)
			authGroup.POST("/logout", authHandlers.Logout)
		}

		// Public Telegram webhook ingress (multi-bot-gateways):
		// no auth middleware — the request authenticates with the
		// derived secret in X-Telegram-Bot-Api-Secret-Token
		// (validated inside the handler before any processing).
		api.POST("/webhooks/telegram/:gatewayId", gatewayHandlers.WebhookUpdate)

		// Public WhatsApp webhook ingress (multi-bot-gateways):
		// no auth middleware — POST authenticates on the
		// X-Hub-Signature-256 HMAC over the raw body (app secret from the
		// gateway's decrypted envelope, validated before any parsing); GET
		// answers the Meta verification handshake (hub.challenge echo).
		api.GET("/webhooks/whatsapp/:gatewayId", gatewayHandlers.WhatsAppWebhookVerify)
		api.POST("/webhooks/whatsapp/:gatewayId", gatewayHandlers.WhatsAppWebhookUpdate)

		// Authenticated endpoints
		authed := api.Group("")
		if rt.opts.Issuer != nil && rt.opts.Store != nil {
			authed.Use(rt.mw.AuthRequired())
		}
		{
			// Auth me
			authed.GET("/auth/me", authHandlers.Me)

			// Workspaces (unscoped user view & creation)
			authed.GET("/workspaces", workspaceHandlers.ListWorkspaces)
			authed.POST("/workspaces", workspaceHandlers.CreateWorkspace)

			// Credential preview for onboarding
			authed.POST("/providers/models-preview", providerHandlers.ModelsPreview)

			// Users self-profile & avatar management
			authed.PATCH("/users/me", userHandlers.PatchMe)
			authed.POST("/users/me/avatar", userHandlers.UploadAvatar)

			// Tenant-scoped routes under /workspaces/:ws
			wsGroup := authed.Group("/workspaces/:ws")
			if rt.opts.Store != nil {
				wsGroup.Use(rt.mw.RequireWorkspace("ws"))
			}
			{
				wsGroup.GET("", rt.mw.RequirePermission(domain.WorkspaceRead), workspaceHandlers.GetWorkspace)
				wsGroup.PATCH("", rt.mw.RequirePermission(domain.WorkspaceWrite), workspaceHandlers.PatchWorkspace)

				// Memory (design D8): own user memory is membership-only and
				// self-scoped to the authenticated caller; shared workspace
				// memory reads ride membership, writes use the same
				// workspace.write gate as the workspace PATCH above.
				wsGroup.GET("/me/memory", memoryHandlers.GetUserMemory)
				wsGroup.PUT("/me/memory", memoryHandlers.PutUserMemory)
				wsGroup.GET("/memory", memoryHandlers.GetWorkspaceMemory)
				wsGroup.PUT("/memory", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryHandlers.PutWorkspaceMemory)

				// Extracted memory management (integrate-agent-zero-memory
				// tasks 5.4): reads — the notes browser, note provenance, the
				// episodic timeline, and the settings view — ride membership
				// like the shared GET above, scope-filtered structurally by
				// the store (viewer = the HTTP caller, no agent impersonation).
				// Writes — audited promotion, tombstone delete, consolidate-
				// now, settings PUT, and the connection test — sit behind the
				// same workspace.write gate.
				wsGroup.GET("/memory/notes", memoryNoteHandlers.ListNotes)
				wsGroup.GET("/memory/notes/:id", memoryNoteHandlers.GetNote)
				wsGroup.POST("/memory/notes/:id/promote", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryNoteHandlers.PromoteNote)
				wsGroup.DELETE("/memory/notes/:id", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryNoteHandlers.DeleteNote)
				wsGroup.GET("/memory/events", memoryNoteHandlers.ListEvents)
				wsGroup.POST("/memory/consolidate", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryNoteHandlers.ConsolidateNow)
				wsGroup.GET("/memory/report", memoryNoteHandlers.GetReport)
				wsGroup.GET("/memory/settings", memoryNoteHandlers.GetSettings)
				wsGroup.PUT("/memory/settings", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryNoteHandlers.PutSettings)
				wsGroup.POST("/memory/settings/test", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryNoteHandlers.TestMemorySettings)

				// Chat attachment upload (attachments design D1):
				// membership-level auth — any workspace member attaches
				// files to their own turns; the returned capability URL is
				// the attachment's wire token.
				wsGroup.POST("/attachments", attachmentHandlers.Upload)

				// Workspace blob-storage configuration (attachments design
				// D16, the settings Storage pane): viewing the masked
				// config and changing it both require workspace settings
				// management (workspace.write, Owner/Admin).
				wsGroup.GET("/storage", rt.mw.RequirePermission(domain.WorkspaceWrite), storageConfigHandlers.GetStorage)
				wsGroup.PUT("/storage", rt.mw.RequirePermission(domain.WorkspaceWrite), storageConfigHandlers.PutStorage)
				// Probe-only "Test connection": same body and validation as
				// PUT, never persists.
				wsGroup.POST("/storage/probe", rt.mw.RequirePermission(domain.WorkspaceWrite), storageConfigHandlers.ProbeStorage)

				wsGroup.GET("/roles", rt.mw.RequirePermission(domain.RolesRead), roleHandlers.ListRoles)

				wsGroup.GET("/members", rt.mw.RequirePermission(domain.MembersRead), memberHandlers.ListMembers)
				wsGroup.POST("/members", rt.mw.RequirePermission(domain.MembersWrite), memberHandlers.AddMember)
				wsGroup.PATCH("/members/:uid", rt.mw.RequirePermission(domain.MembersWrite), memberHandlers.PatchMember)
				wsGroup.DELETE("/members/:uid", memberHandlers.DeleteMember)

				// Providers management
				wsGroup.GET("/providers", rt.mw.RequirePermission(domain.ProvidersRead), providerHandlers.ListProviders)
				wsGroup.POST("/providers", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.CreateProvider)
				wsGroup.PATCH("/providers/:id", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.PatchProvider)
				wsGroup.DELETE("/providers/:id", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.DeleteProvider)
				wsGroup.POST("/providers/:id/verify", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.VerifyProvider)
				// Draft verify for the provider dialog (refactor D5): probes the
				// unsaved form values — the stored key when editing with a blank
				// key field — and persists nothing.
				wsGroup.POST("/providers/verify-draft", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.VerifyDraft)
				wsGroup.GET("/providers/:id/models", rt.mw.RequirePermission(domain.ProvidersRead), providerHandlers.GetProviderModels)

				// Agents management
				wsGroup.GET("/agents", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.ListAgents)
				wsGroup.POST("/agents", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.CreateAgent)
				wsGroup.GET("/agents/:agent", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.GetAgent)
				wsGroup.PATCH("/agents/:agent", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.PatchAgent)
				wsGroup.DELETE("/agents/:agent", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.DeleteAgent)
				wsGroup.POST("/agents/:agent/regenerate", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.RegenerateAgent)
				// Durable agent session index (agent-session-index D3): the
				// requesting user's non-deleted sessions with the live-run
				// flag, and the soft delete. Listing rides agents.read like
				// the transcript read below; deletion is agents.write.
				wsGroup.GET("/agents/:agent/sessions", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.ListAgentSessions)
				wsGroup.DELETE("/agents/:agent/sessions/:session", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.DeleteAgentSession)
				wsGroup.GET("/agents/:agent/sessions/:session/events", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.ListSessionEvents)
				wsGroup.POST("/agents/:agent/sessions/:session/approvals/:interruptID", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.ResolveApproval)
				wsGroup.POST("/agents/:agent/sessions/:session/runs/:turn/cancel", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.CancelRun)

				// Agent workspace files (add-right-panel 2.4): byte reads and
				// one-level listings from the agent's jail directory. Reads
				// ride agents.read like the transcript endpoints above; the
				// workspace in scope is the authenticated member's context,
				// never the URL slug.
				wsGroup.GET("/agents/:agent/files", rt.mw.RequirePermission(domain.AgentsRead), workspaceFilesHandlers.ServeWorkspaceFiles)

				// Tool settings (workspace settings; read covered by membership,
				// writes are Owner/Admin via tools.write)
				wsGroup.GET("/tools", toolSettingsHandlers.ListTools)
				wsGroup.PATCH("/tools/:key", rt.mw.RequirePermission(domain.ToolsWrite), toolSettingsHandlers.PatchTool)

				// Workspace MCP registry (reads ride membership like /tools —
				// every built-in role holds tools.read; writes are Owner/Admin
				// via tools.write, design.md D9)
				wsGroup.GET("/mcp-servers", mcpServerHandlers.ListWorkspaceServers)
				wsGroup.POST("/mcp-servers", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.CreateWorkspaceServer)
				wsGroup.PATCH("/mcp-servers/:id", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.PatchWorkspaceServer)
				wsGroup.DELETE("/mcp-servers/:id", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.DeleteWorkspaceServer)
				wsGroup.POST("/mcp-servers/:id/probe", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.ProbeWorkspaceServer)

				// Workspace service connections (add-workspace-connections
				// D9/D10): the recipes gallery and connection reads ride
				// membership; connect, probe, and disconnect are
				// integrations.write — the credential-bearing trust tier,
				// held by built-in Owner/Admin (Superadmin via its all-
				// workspace-permissions set), never Member, and by custom
				// roles only on explicit grant.
				wsGroup.GET("/integrations/recipes", connectionsHandlers.ListRecipes)
				wsGroup.GET("/integrations/connections", connectionsHandlers.ListConnections)
				wsGroup.GET("/integrations/connections/:id", connectionsHandlers.GetConnection)
				wsGroup.POST("/integrations/connections", rt.mw.RequirePermission(domain.IntegrationsWrite), connectionsHandlers.Connect)
				wsGroup.DELETE("/integrations/connections/:id", rt.mw.RequirePermission(domain.IntegrationsWrite), connectionsHandlers.Disconnect)
				wsGroup.POST("/integrations/connections/:id/probe", rt.mw.RequirePermission(domain.IntegrationsWrite), connectionsHandlers.ProbeConnection)

				// Workspace skill library (registry + system tier; Member reads,
				// Owner/Admin/Superadmin manage via skills.write)
				wsGroup.GET("/skills", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.ListSkills)
				wsGroup.POST("/skills", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.CreateSkill)
				wsGroup.POST("/skills/inspect", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InspectUpload)
				wsGroup.POST("/skills/inspect/git", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InspectGit)
				wsGroup.GET("/skills/:name", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.GetSkill)
				wsGroup.PUT("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.UpdateSkill)
				wsGroup.PATCH("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.PatchSkill)
				wsGroup.DELETE("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.DeleteSkill)
				wsGroup.POST("/skills/:name/dependencies/recheck", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.RecheckSkill)

				// Agent-tier skills: install/remove under the agent endpoints
				// (presence on disk is the state)
				wsGroup.GET("/agents/:agent/skills", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.ListAgentSkills)
				wsGroup.POST("/agents/:agent/skills", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InstallAgentSkill)
				wsGroup.DELETE("/agents/:agent/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.RemoveAgentSkill)

				// Agent-private MCP servers (agents.write per design.md D9;
				// reads ride agents.read like the agent detail endpoint)
				wsGroup.GET("/agents/:agent/mcp-servers", rt.mw.RequirePermission(domain.AgentsRead), mcpServerHandlers.ListAgentServers)
				wsGroup.POST("/agents/:agent/mcp-servers", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.CreateAgentServer)
				wsGroup.PATCH("/agents/:agent/mcp-servers/:id", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.PatchAgentServer)
				wsGroup.DELETE("/agents/:agent/mcp-servers/:id", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.DeleteAgentServer)
				wsGroup.POST("/agents/:agent/mcp-servers/:id/probe", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.ProbeAgentServer)

				// Channel CRUD (integrate-agent-channels D11/D12): reads ride
				// channels.read, writes channels.write. The static
				// /channels/templates route is registered BEFORE /channels/:id
				// so gin resolves it literally (no :id capture).
				wsGroup.GET("/channels/templates", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.ListChannelTemplates)
				wsGroup.POST("/channels/templates/:template/materialize", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.MaterializeTemplate)
				wsGroup.GET("/channels", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.ListChannels)
				wsGroup.POST("/channels", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.CreateChannel)
				wsGroup.GET("/channels/:id", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.GetChannel)
				wsGroup.PATCH("/channels/:id", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.PatchChannel)
				wsGroup.DELETE("/channels/:id", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.DeleteChannel)

				// Channel membership roster.
				wsGroup.GET("/channels/:id/members", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.ListChannelMembers)
				wsGroup.POST("/channels/:id/members", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.AddChannelMember)
				wsGroup.PATCH("/channels/:id/members/:mid", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.PatchChannelMember)
				wsGroup.DELETE("/channels/:id/members/:mid", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.RemoveChannelMember)

				// Channel feed: cursor reads plus chokepoint posts (with the
				// kickoff flag, channel-teams D1), work-session reads, and the
				// live SSE event stream.
				wsGroup.GET("/channels/:id/messages", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.ListMessages)
				wsGroup.POST("/channels/:id/messages", rt.mw.RequirePermission(domain.ChannelsWrite), channelHandlers.PostMessage)
				wsGroup.GET("/channels/:id/sessions", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.ListChannelSessions)
				wsGroup.GET("/channels/:id/sessions/:sid", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.GetChannelSession)
				wsGroup.GET("/channels/:id/events", rt.mw.RequirePermission(domain.ChannelsRead), channelHandlers.StreamEvents)

				// Scheduler standing orders (integrate-scheduler D11): reads
				// ride scheduler.read, mutations and run-now scheduler.write.
				// /scheduler-runs (workspace-wide history) is a distinct
				// literal segment from /schedulers/:id, so registration order
				// carries no meaning here.
				wsGroup.GET("/schedulers", rt.mw.RequirePermission(domain.SchedulerRead), schedulerHandlers.ListSchedulers)
				wsGroup.POST("/schedulers", rt.mw.RequirePermission(domain.SchedulerWrite), schedulerHandlers.CreateScheduler)
				wsGroup.GET("/scheduler-runs", rt.mw.RequirePermission(domain.SchedulerRead), schedulerHandlers.ListWorkspaceSchedulerRuns)
				wsGroup.GET("/schedulers/:id", rt.mw.RequirePermission(domain.SchedulerRead), schedulerHandlers.GetScheduler)
				wsGroup.PATCH("/schedulers/:id", rt.mw.RequirePermission(domain.SchedulerWrite), schedulerHandlers.PatchScheduler)
				wsGroup.DELETE("/schedulers/:id", rt.mw.RequirePermission(domain.SchedulerWrite), schedulerHandlers.DeleteScheduler)
				wsGroup.POST("/schedulers/:id/run", rt.mw.RequirePermission(domain.SchedulerWrite), schedulerHandlers.RunSchedulerNow)
				wsGroup.GET("/schedulers/:id/runs", rt.mw.RequirePermission(domain.SchedulerRead), schedulerHandlers.ListSchedulerRuns)

				// Agent heartbeat (add-agent-heartbeat D13): agent sub-resource
				// riding the agents permissions — read agents.read, every
				// mutation (create/update, run-now, resume) agents.write; no
				// new permission catalog entries. Literal segments
				// (heartbeat/run-now, heartbeat/resume) are siblings of
				// /agents/:agent/* reads, so registration order carries no
				// meaning.
				wsGroup.GET("/agents/:agent/heartbeat", rt.mw.RequirePermission(domain.AgentsRead), heartbeatHandlers.GetAgentHeartbeat)
				wsGroup.PUT("/agents/:agent/heartbeat", rt.mw.RequirePermission(domain.AgentsWrite), heartbeatHandlers.PutAgentHeartbeat)
				wsGroup.POST("/agents/:agent/heartbeat/run-now", rt.mw.RequirePermission(domain.AgentsWrite), heartbeatHandlers.RunAgentHeartbeatNow)
				wsGroup.POST("/agents/:agent/heartbeat/resume", rt.mw.RequirePermission(domain.AgentsWrite), heartbeatHandlers.ResumeAgentHeartbeat)

				// API keys management (workspace settings; workspace.write is Owner/Admin only)
				// Exchange is Member-level: membership via RequireWorkspace suffices,
				// like the other member-readable routes (e.g. GET /tools).
				wsGroup.POST("/api-keys/exchange", apiKeyHandlers.ExchangeAPIKey)
				wsGroup.GET("/api-keys", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.ListAPIKeys)
				wsGroup.POST("/api-keys", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.CreateAPIKey)
				wsGroup.DELETE("/api-keys/:id", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.RevokeAPIKey)

				// Workspace agent lifecycle hooks (D13/D17): reads ride
				// hooks.read, writes hooks.write. The instance section of the
				// list is read-only visibility; the dry-run endpoint executes
				// the real handler against a synthetic event.
				wsGroup.GET("/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHooks)
				wsGroup.POST("/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.CreateWorkspaceHook)
				wsGroup.POST("/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.ReorderWorkspaceHooks)
				wsGroup.POST("/hooks/test", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.TestWorkspaceHook)
				wsGroup.GET("/hooks/executions", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHookExecutions)
				wsGroup.GET("/hooks/:id/executions", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHookExecutions)
				wsGroup.PATCH("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.PatchWorkspaceHook)
				wsGroup.DELETE("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.DeleteWorkspaceHook)

				// Agent-level hooks (agent config modal; private to the
				// owning agent, D13): same permission contract as the
				// workspace level.
				wsGroup.GET("/agents/:agent/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListAgentLevelHooks)
				wsGroup.POST("/agents/:agent/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.CreateAgentHook)
				wsGroup.POST("/agents/:agent/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.ReorderAgentHooks)
				wsGroup.PATCH("/agents/:agent/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.PatchAgentHook)
				wsGroup.DELETE("/agents/:agent/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.DeleteAgentHook)

				// Telegram gateway (multi-bot-gateways):
				// account CRUD, enable/disable/test, bindings, and the admin
				// per-member unpair are gateways.write (Owner/Admin; the web
				// pane mirrors the same check). Pairing-token mint/revoke and
				// the member's own link are member-level — the workspace
				// context gate suffices, matching the api-keys/exchange
				// precedent. /links/me resolves literally before
				// /links/:uid; registration order carries no meaning.
				gatewayGroup := wsGroup.Group("/gateways/telegram")
				{
					gatewayGroup.GET("", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.ListTelegramGateways)
					gatewayGroup.POST("", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.CreateTelegramGateway)
					gatewayGroup.GET("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.GetTelegramGateway)
					gatewayGroup.PUT("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.UpdateTelegramGateway)
					gatewayGroup.POST("/:gatewayId/enable", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.EnableTelegramGateway)
					gatewayGroup.POST("/:gatewayId/disable", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.DisableTelegramGateway)
					gatewayGroup.POST("/:gatewayId/test", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.TestTelegramGateway)
					gatewayGroup.DELETE("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.DeleteTelegramGateway)
					gatewayGroup.GET("/bindings", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.ListBindings)
					gatewayGroup.POST("/bindings", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.CreateBinding)
					gatewayGroup.DELETE("/bindings/:id", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.DeleteBinding)
					gatewayGroup.POST("/pairing-tokens", gatewayHandlers.CreatePairingToken)
					gatewayGroup.DELETE("/pairing-tokens/:token", gatewayHandlers.RevokePairingToken)
					gatewayGroup.GET("/links/me", gatewayHandlers.GetMyLink)
					gatewayGroup.DELETE("/links/me", gatewayHandlers.UnpairMyLink)
					gatewayGroup.DELETE("/links/:uid", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.UnpairMemberLink)
				}

				// WhatsApp gateway (multi-bot-gateways):
				// lane-scoped account CRUD, enable/disable, health, and the
				// multi-device pairing state machine are gateways.write
				// (Owner/Admin); pairing tokens and the member's own link are
				// member-level — the same split as the telegram group above,
				// mirrored by the web pane (api.gateways.whatsapp).
				whatsappGroup := wsGroup.Group("/gateways/whatsapp")
				{
					whatsappGroup.GET("", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.ListWhatsAppGateways)
					whatsappGroup.POST("", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.CreateWhatsAppGateway)
					whatsappGroup.GET("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.GetWhatsAppGateway)
					whatsappGroup.PUT("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.UpdateWhatsAppGateway)
					whatsappGroup.POST("/:gatewayId/enable", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.EnableWhatsAppGateway)
					whatsappGroup.POST("/:gatewayId/disable", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.DisableWhatsAppGateway)
					whatsappGroup.GET("/:gatewayId/health", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.GetWhatsAppHealth)
					whatsappGroup.POST("/:gatewayId/pairing/start", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.StartWhatsAppPairing)
					whatsappGroup.GET("/:gatewayId/pairing/status", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.GetWhatsAppPairingStatus)
					whatsappGroup.POST("/:gatewayId/pairing/regenerate", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.RegenerateWhatsAppPairing)
					whatsappGroup.POST("/:gatewayId/pairing/logout", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.LogoutWhatsAppDevice)
					whatsappGroup.DELETE("/:gatewayId", rt.mw.RequirePermission(domain.GatewaysWrite), gatewayHandlers.DeleteWhatsAppGateway)
					whatsappGroup.POST("/pairing-tokens", gatewayHandlers.CreatePairingToken)
					whatsappGroup.DELETE("/pairing-tokens/:token", gatewayHandlers.RevokePairingToken)
					whatsappGroup.GET("/links/me", gatewayHandlers.GetMyWhatsAppLink)
					whatsappGroup.DELETE("/links/me", gatewayHandlers.UnpairMyWhatsAppLink)
				}
			}

			// Instance Admin route group (master tenant control plane)
			adminGroup := authed.Group("/admin")
			if rt.opts.Store != nil {
				adminGroup.Use(rt.mw.RequireMasterWorkspace())
			}
			{
				// Workspaces management
				adminGroup.GET("/workspaces", rt.mw.RequirePermission(domain.AdminWorkspacesRead), adminWorkspaceHandlers.AdminListWorkspaces)
				adminGroup.POST("/workspaces", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminCreateWorkspace)
				adminGroup.PATCH("/workspaces/:ws", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminPatchWorkspace)
				adminGroup.PATCH("/workspaces/:ws/owner", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminTransferWorkspaceOwner)
				adminGroup.GET("/workspaces/:ws/members", rt.mw.RequirePermission(domain.AdminWorkspacesRead), adminWorkspaceHandlers.AdminListWorkspaceMembers)
				adminGroup.POST("/workspaces/:ws/members", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminAddWorkspaceMember)
				adminGroup.POST("/workspaces/:ws/disable", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminDisableWorkspace)
				adminGroup.POST("/workspaces/:ws/enable", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminEnableWorkspace)

				// Users management
				adminGroup.GET("/users", rt.mw.RequirePermission(domain.AdminUsersRead), adminUserHandlers.AdminListUsers)
				adminGroup.POST("/users", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminCreateUser)
				adminGroup.POST("/users/:uid/disable", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminDisableUser)
				adminGroup.POST("/users/:uid/enable", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminEnableUser)

				// Superadmins management
				adminGroup.POST("/superadmins", rt.mw.RequirePermission(domain.AdminSuperadminsWrite), adminSuperadminHandlers.AdminGrantSuperadmin)
				adminGroup.DELETE("/superadmins/:uid", rt.mw.RequirePermission(domain.AdminSuperadminsWrite), adminSuperadminHandlers.AdminRevokeSuperadmin)

				// Instance hooks (D13/D15/D17): managed rows under superadmin
				// CRUD plus the read-only builtin listing. Hooks permissions
				// gate the surface (the built-in master-tenant Superadmin role
				// holds them via the permission backfill); no workspace scope
				// anywhere in these paths.
				adminGroup.GET("/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.AdminListHooks)
				adminGroup.POST("/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminCreateHook)
				adminGroup.POST("/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminReorderHooks)
				adminGroup.PATCH("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminPatchHook)
				adminGroup.DELETE("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminDeleteHook)
			}
		}
	}

	return r
}

// NewRouter builds and returns the configured Gin Engine.
func NewRouter(opts RouterOptions) *gin.Engine {
	return New(opts).Engine()
}
