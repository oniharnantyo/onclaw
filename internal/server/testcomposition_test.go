package server_test

// Test composition helper (fix-hooks-dispatcher-unwired task 2.3): the
// router's `if runner == nil` fallback runner construction was deleted —
// RuntimeOptions.Runner is a required, non-nil dependency (house rule: the
// composition root resolves every dependency) and the deleted option list was
// the second NewRunner site whose divergence from the CLI left the hooks
// dispatcher unwired in production.
//
// Tests that booted RouterOptions without a Runner relied on that fallback as
// a convenience. This helper recreates it as explicit TEST composition — the
// same option list the deleted fallback assembled, one construction site,
// never production code. Boot sites pass the returned runner (and, when the
// test drives channel lanes, the channel runtime) to server.RouterOptions.

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	mcpoauth "github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// newTestRunnerComposition builds the fallback-equivalent runner over the
// given stores plus the channel runtime it is late-bound to. dataDir roots
// the channel project space; onClawDir roots the runner's skill/wiki
// directories. extra options append AFTER the fallback list (a test may
// override the model factory, for example). The deleted fallback also wired
// the router's fallback authorizer (runner-side connection-gate re-check);
// no rehomed test exercises that lane, so the helper omits it.
func newTestRunnerComposition(
	t *testing.T,
	st store.Store,
	stor storage.Storage,
	encKey []byte,
	dataDir, onClawDir string,
	extra ...agents.RunnerOption,
) (*agents.Runner, *server.ChannelRuntime) {
	t.Helper()
	memLog := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Memory pipeline (integrate-agent-zero-memory): consumer #1 of the
	// neutral ingest worker. The worker is never Started here — the drain
	// loop rides the composition root's lifecycle — so the extra curation
	// consumer the deleted fallback registered is inert in tests and omitted.
	memWorker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), encKey, providers.NewRegistry()),
		st.MemoryEmbeddings(),
		memLog,
	)
	ingestWorker := ingest.NewWorker(memLog, ingest.WithConsumers(memWorker))
	memSearch := newTestMemorySearcher(st)
	memGate := memory.NewIntentGate(st.Providers(), encKey, agents.DefaultAgenticModelFactory, memLog, memory.WithWorkspaceModelSource(st.ToolSettings()))

	// MCP runtime dependencies: fresh ones over the granular stores, the
	// fallback shapes.
	mcpSettings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), encKey)
	mcpOAuthClient := mcpoauth.NewClient(encKey)
	mcpOAuthCreds := services.NewMCPOAuthDialCredentials(
		st.MCPTokens(), st.WorkspaceMCPServers(), st.AgentMCPServers(),
		mcpSettings, encKey, "", mcpOAuthClient,
	)
	mcpManager := mcp.NewMCPManager(mcp.WithOAuthCredentials(mcpOAuthCreds))
	t.Cleanup(mcpManager.Close)
	mcpOAuthRefresher := services.NewMCPOAuthTokenRefresher(
		st.MCPTokens(), st.WorkspaceMCPServers(), st.AgentMCPServers(),
		mcpSettings, encKey, "", mcpOAuthClient,
	)
	connectionsSvc := services.NewConnectionsService(
		st.Connections(), st.WorkspaceMCPServers(), st.Agents(), mcpSettings, st.OAuthApps(),
		encKey, "",
		services.WithProbeTimeout(handlers.DefaultMCPProbeTimeout),
		services.WithAttachmentTx(func(ctx context.Context, run func(ctx context.Context, agents store.AgentStore) error) error {
			return st.WithTx(ctx, func(s store.Store) error {
				return run(ctx, s.Agents())
			})
		}),
	)
	runtimeSource := services.NewRuntimeCredentialSource(mcpSettings, connectionsSvc, services.WithMCPOAuthTokenRefresher(mcpOAuthRefresher))

	// Shared hook registry + dispatcher: the same construction the router's
	// fallback consumed, with the kill switches on (the test-assembly
	// default).
	hookRegistry := agenthooks.NewRegistry(
		agenthooks.WithEncryptionKey(encKey),
		agenthooks.WithCommandEnabled(true),
		agenthooks.WithScriptEnabled(true),
		agenthooks.WithMCPInvoker(mcp.NewHooksInvoker(runtimeSource, mcpManager)),
		agenthooks.WithEvaluatorFactory(agents.NewHookEvaluatorFactory(st.Providers(), encKey, agents.DefaultAgenticModelFactory)),
	)

	// Workspace blob storage resolver + references service: the fallback
	// shapes over the instance storage.
	wsStorage := resolver.New(stor, st.WorkspaceStorage(), st.Attachments(), encKey, dataDir)
	referencesSvc := references.NewService(st, wsStorage)

	modelCatalog := services.NewModelCatalog(services.ModelCatalogOptions{Registry: providers.NewRegistry()})

	// Channel fan-out: built here and returned so the boot site can pass the
	// SAME runtime to RouterOptions — the composition-root pattern.
	channelRuntime := server.NewChannelRuntime(st.Channels(), st.WorkSessions(), st.Users(), st.Agents())

	opts := append([]agents.RunnerOption{
		agents.WithToolRegistry(agents.NewDefaultToolRegistry(
			st.Memories(),
			agents.WithSchedulerTools(st.Schedulers(), st.Channels()),
			agents.WithMemorySearch(memSearch),
			agents.WithTodoTools(st.Todos()),
			agents.WithDocumentTools(referencesSvc),
		)),
		agents.WithEnabledSkillReader(server.WorkspaceSkillReader(st.WorkspaceSkills())),
		agents.WithMCPPolicy(mcp.NewSettingsPolicy(runtimeSource)),
		agents.WithMCPManager(mcpManager),
		agents.WithMCPStatusWriter(mcp.NewSettingsStatusWriter(runtimeSource)),
		agents.WithConnectionToolSource(server.NewConnectionToolSource(connectionsSvc)),
		agents.WithConnectionOriginLookup(connectionsSvc),
		agents.WithHooks(agenthooks.NewDispatcher(st.Hooks(), hookRegistry)),
		agents.WithChannelContext(channelRuntime.Chokepoint()),
		agents.WithChannelFeed(channelRuntime.Chokepoint()),
		agents.WithWorkSessions(channelRuntime.Chokepoint()),
		agents.WithProjectSpace(channels.NewLocalProjectSpace(dataDir, st.Workspaces())),
		agents.WithAttachmentBlobs(wsStorage),
		agents.WithInputModalityResolver(modelCatalog),
		agents.WithTodoStore(st.Todos()),
		agents.WithMemoryGateBudget(memory.SettingsGateBudget(st.ToolSettings(), memLog)),
		agents.WithReferences(referencesSvc),
	}, extra...)

	runner := agents.NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), ingestWorker, memSearch, memGate,
		encKey, onClawDir,
		opts...,
	)
	channelRuntime.BindRunner(runner)
	return runner, channelRuntime
}
