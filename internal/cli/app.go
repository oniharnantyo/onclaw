package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/agents/tools/documents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/channels/telegram"
	"github.com/oniharnantyo/buddies/internal/config"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/promptgen"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/skills"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/sqlite"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// App is one assembled application instance: the resolved data layout, the
// open store, the gateway seam, the agent runtime, and the bound service
// set. Both entrypoints (desktop and server) build it through Assemble.
type App struct {
	// Layout is the resolved on-disk data layout (normally ~/.buddies).
	Layout config.Layout
	// Store is the open, migrated store. Close closes it.
	Store store.Store
	// Gateway is the gateway lifecycle seam.
	Gateway gateway.Manager
	// Runtime is the agent runtime.
	Runtime agents.Manager

	// mcpMgr is the MCP connection cache behind the runtime and the
	// settings service; Close tears it down.
	mcpMgr *mcp.MCPManager

	// Services is the bound service set handed to the host.
	Services ServiceSet

	// sched is the attached scheduler; Close stops its tick loop before the
	// gateway and the store go down.
	sched *scheduler.Service

	// memTicker is the attached memory sweep ticker; Close stops its tick
	// loop before the gateway and the store go down.
	memTicker *memory.Ticker

	// tg is the attached Telegram channel; Close stops its goroutines
	// (bounded join, add-telegram-channel design D1) before the gateway and
	// the store go down.
	tg *telegram.Service

	closeOnce sync.Once
	closeErr  error
}

// ServiceSet is the set of services bound to the host's frontend.
type ServiceSet struct {
	AppInfo   *services.AppInfo
	Agents    *services.AgentsService
	Chat      *services.ChatService
	Settings  *services.SettingsService
	Gateway   *services.GatewayService
	Schedule  *services.ScheduleService
	Knowledge *services.KnowledgeService
	Channels  *services.ChannelService
}

// Assemble builds the full application over the data layout resolved from
// the user's home directory: it creates the layout, opens the store (the
// migration gate — a failure here is fatal to startup), and constructs the
// gateway, runtime, and services. emitter receives every service event and
// opener launches the services' system-browser flows; both must be non-nil
// (services.Noop and services.NoBrowser for hosts without those
// capabilities).
func Assemble(ctx context.Context, emitter services.Emitter, opener services.URLOpener) (*App, error) {
	layout, err := config.Resolve()
	if err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}
	if err := layout.Ensure(); err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}

	st, err := sqlite.Open(ctx, layout.DBPath)
	if err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}

	gw := gateway.NewManager()

	// The agent workspaces root backs both the runtime (documents read
	// fresh per turn) and the services' prompt lifecycle (generation
	// commits, document editors, delete cleanup).
	wsMgr := workspaces.NewManager(layout.Workspaces)
	promptGen := promptgen.NewService(promptgen.NewDefaultModelFactory(), services.NewWorkspaceDocStore(wsMgr))

	// The durable knowledge corpus store (add-knowledge-corpus design D2):
	// raw document bytes under the layout's knowledge directory, cataloged
	// through the store's documents table. It backs the runtime's
	// save_document tool and /knowledge mount, the bound Knowledge service,
	// and the agents service's delete cleanup.
	knowStore := knowledge.NewStore(layout.Knowledge, st.Documents())
	agentsSvc := services.NewAgentsService(st, promptGen, wsMgr, knowStore, layout.Skills)

	// Sweep crash-orphaned prompt generations before services accept calls
	// (design D4): rows still in generating are moved to failed with the
	// canonical interrupted reason. Cosmetic to startup — a sweep failure
	// only leaves retryable rows stuck — so it is logged, not fatal.
	if err := agentsSvc.SweepInterruptedGenerations(ctx); err != nil {
		slog.Warn("assemble: prompt generation sweep failed", "error", err)
	}

	// The MCP connection cache backs both the runtime's per-turn tool
	// surface (WithMCPManager) and the settings service's invalidate-on-write
	// seam: an edited, disabled, or removed definition drops its cached
	// connection immediately (design D7).
	mcpMgr := mcp.NewMCPManager()
	// The hook dispatcher gates and observes every turn (spec agent-hooks):
	// resolution reads both assignment tiers fresh per turn, and both
	// handler types are enabled (design D9).
	hooksDispatcher := agents.NewHooksDispatcher(st)

	// The scheduler's cron brain (add-scheduler design D1). The construction
	// cycle resolves late-bound: the runtime's scheduler tool needs this
	// service, and this adapter needs the runtime, so the adapter's fields
	// are filled in below — nothing calls it before Assemble returns.
	runs := &scheduleRuns{}
	sched := scheduler.New(st, runs, gw)

	runtime := agents.NewManager(st,
		agents.WithPromptDocs(agents.NewWorkspacePromptDocs(wsMgr)),
		agents.WithWorkspaces(wsMgr),
		agents.WithSkills(layout.Skills),
		agents.WithAttachments(layout.Attachments),
		agents.WithKnowledge(layout.Knowledge),
		agents.WithBrowserDir(layout.Browser),
		agents.WithMCPManager(mcpMgr),
		agents.WithHooks(hooksDispatcher),
		agents.WithScheduler(sched),
		// Turn transitions announce app-wide (add-grouped-session-rail
		// design D2): the runtime core is where UI dispatch, scheduler fire,
		// and gateway trigger converge.
		agents.WithTurnTransition(func(sessionID string, busy bool) {
			emitter.Emit(services.EventTurnsChanged, services.TurnsChangedEvent{SessionID: sessionID, Busy: busy})
		}))
	sessionSvc := sessions.NewService(st)
	runs.runtime, runs.sessions = runtime, sessionSvc
	// The minute-aligned tick loop starts on the gateway's first Running
	// transition (design D2); both hosts start the gateway after Assemble.
	sched.Attach(gw)

	// The memory consolidation sweep ticks hourly per agent while the
	// gateway is Running (add-agent-memory design D7), skipping ticks while
	// Paused; both hosts start the gateway after Assemble.
	memTicker := memory.NewTicker(st, wsMgr)
	memTicker.Attach(gw)
	// The memory block's staleness check reads the same gateway the sweep
	// ticker follows (add-agent-memory design D13): no successful sweep
	// within two intervals while Running is the stale notice; while the
	// gateway is down, cold is not dead.
	agents.SetMemoryGatewayRunning(func() bool { return gw.State() == gateway.StateRunning })

	// The capability-gated attachment store (design D5) serves the chat
	// attachments feature from the layout's attachments directory.
	attStore := attachments.NewStore(layout.Attachments, st.Attachments())

	// The Telegram channel (add-telegram-channel design D1): the bot token's
	// poll loop, authorization, chat-to-session mapping, and outbound
	// delivery over the store's channel row. Construction is wiring only —
	// nothing polls until Attach observes a Running gateway below, and a
	// disabled channel row boots as a clean no-op (design D10).
	tg := telegram.New(telegramRuntime{runtime}, sessionSvc, st.Channels(), st.GatewaySettings(), attStore,
		// Turn artifacts resolve to bytes for sendDocument delivery (design
		// D12): the agent store names the workspace's owner, the workspace
		// manager reads the file jailed to that workspace.
		telegram.WithArtifacts(telegramArtifacts{agents: st.Agents(), ws: wsMgr}),
		// Connection-state transitions announce app-wide (design D10): the
		// channel's State* strings are the services' ChannelState* values.
		telegram.WithStateSink(func(channelID, state, detail string) {
			emitter.Emit(services.EventChannelState, services.ChannelStateEvent{
				ChannelID: channelID,
				State:     state,
				Detail:    detail,
			})
		}))
	// Run completions announce to the home chat (add-telegram-channel design
	// D13), bound into the schedule adapter the same late-bound way as
	// runs.runtime above.
	runs.announcer = tg
	// The channel runtime starts on the gateway's first Running transition
	// (design D1, the scheduler ticker's attach pattern); both hosts start
	// the gateway after Assemble.
	tg.Attach(gw)

	// Index the on-disk skill library into the registry. Cosmetic to
	// startup: the skills directory may be empty, and one broken skill
	// directory is logged and skipped inside the scan — never fatal.
	if err := skills.Scan(ctx, st.Skills(), layout.Skills, slog.Default()); err != nil {
		slog.Warn("assemble: skill directory scan failed", "error", err)
	}

	settingsSvc := services.NewSettingsService(st,
		services.WithMCPInvalidate(mcpMgr.Invalidate),
		// The browser profile management roots at the layout's browser
		// directory (spec add-browser-tool D10) — the same root the
		// runtime's browser tool was given above.
		services.WithBrowserDir(layout.Browser),
		// The MCP OAuth flow announces completion through the host's event
		// bus (design D2) and opens its authorization page through the
		// host's browser seam (design D8).
		services.WithEmitter(emitter),
		services.WithURLOpener(opener))
	// Seed canonical provider models if missing (design D3)
	if err := settingsSvc.SeedCanonicalModels(ctx); err != nil {
		slog.Warn("assemble: provider model seed failed", "error", err)
	}

	// Seed the knowledge-search skill into the global skills tier when
	// missing (add-knowledge-corpus design D10, repair-if-deleted). Cosmetic
	// to startup — the corpus works without the playbook — so it is logged,
	// not fatal.
	if err := knowledge.SeedSkill(layout.Skills); err != nil {
		slog.Warn("assemble: knowledge skill seed failed", "error", err)
	}

	// Seed the documents skill into the global skills tier when missing
	// (add-document-editing design D10, task 5.3 — the same
	// repair-if-deleted pattern, mirroring the knowledge seed above).
	// Cosmetic to startup — document work works without the playbook — so it
	// is logged, not fatal.
	if err := documents.SeedDocumentsSkill(layout.Skills); err != nil {
		slog.Warn("assemble: documents skill seed failed", "error", err)
	}

	// Seed the memory skill into the global skills tier when missing
	// (add-agent-memory tasks 3.1, repair-if-deleted — mirroring the
	// knowledge seed above). Cosmetic to startup — curation works without
	// the playbook — so it is logged, not fatal.
	if err := memory.SeedSkill(layout.Skills); err != nil {
		slog.Warn("assemble: memory skill seed failed", "error", err)
	}

	// Seed the create-skill skill into the global skills tier when missing
	// (add-agent-created-skills design D1, repair-if-deleted — mirroring the
	// knowledge seed above). Cosmetic to startup — skill authoring works
	// without the playbook — so it is logged, not fatal.
	if err := agents.SeedCreateSkill(layout.Skills); err != nil {
		slog.Warn("assemble: create-skill seed failed", "error", err)
	}

	return &App{
		Layout:    layout,
		Store:     st,
		Gateway:   gw,
		Runtime:   runtime,
		mcpMgr:    mcpMgr,
		sched:     sched,
		memTicker: memTicker,
		tg:        tg,
		Services: ServiceSet{
			AppInfo: services.NewAppInfo(),
			Agents:  agentsSvc,
			Chat: services.NewChatService(runtime, sessionSvc, emitter, st, attStore, wsMgr,
				// The browser-screenshot read serves from the layout's
				// browser directory (design D6) — the same root the
				// runtime's browser tool was given above.
				services.WithChatBrowserDir(layout.Browser)),
			Settings:  settingsSvc,
			Gateway:   services.NewGatewayService(gw, emitter),
			Schedule:  services.NewScheduleService(sched, emitter),
			Knowledge: services.NewKnowledgeService(knowStore, st.Agents(), emitter),
			Channels:  services.NewChannelService(st, emitter),
		},
	}, nil
}

// telegramRuntime adapts the agent runtime onto the Telegram channel's
// Runtime seam (add-telegram-channel design D5/D6). Every method delegates to
// the manager unchanged except Submit, whose concrete *agents.Turn satisfies
// the channel's TurnView structurally (telegram deps.go) but needs the
// interface-shape bridge Go requires.
type telegramRuntime struct {
	agents.Manager
}

// Submit adapts the manager's *agents.Turn onto telegram.TurnView.
func (t telegramRuntime) Submit(ctx context.Context, req agents.SendRequest) (telegram.TurnView, error) {
	return t.Manager.Submit(ctx, req)
}

// telegramArtifacts adapts the agent store and the workspace manager onto the
// Telegram channel's Artifacts seam (add-telegram-channel design D12): the
// turn's agent id resolves to its workspace slug, and the artifact's
// workspace-relative path reads through the jailed workspace reader — the
// same reader the chat service serves workspace files with, so traversal and
// symlink escapes are refused before anything leaves the machine.
type telegramArtifacts struct {
	agents store.AgentStore
	ws     *workspaces.Manager
}

var _ telegram.Artifacts = (*telegramArtifacts)(nil)

// Read returns one artifact's bytes from the agent's workspace.
func (t telegramArtifacts) Read(ctx context.Context, agentID, relPath string) ([]byte, error) {
	agent, err := t.agents.ByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("telegram artifacts: agent %s: %w", agentID, err)
	}
	content, err := t.ws.ReadFile(agent.Slug, relPath)
	if err != nil {
		return nil, fmt.Errorf("telegram artifacts: read %s: %w", relPath, err)
	}
	return []byte(content), nil
}

// Close shuts the application down: it stops the scheduler's tick loop and
// the Telegram channel's goroutines (so no tick or channel goroutine races
// the teardown below), stops the gateway (unless already stopped), tears down
// the MCP connection cache (reaping any stdio children), and closes the
// store. It is idempotent; the first call's result is returned on every call.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		var errs []error
		a.sched.Stop()
		// The Telegram channel stops with its bounded join (add-telegram-
		// channel design D1): a hung API call must not block the gateway or
		// store teardown below.
		a.tg.Stop()
		// The memory sweep ticker's loop stops first too, so no sweep races
		// the gateway or store teardown below (an in-flight sweep completes
		// before Stop returns).
		a.memTicker.Stop()
		if a.Gateway.State() != gateway.StateStopped {
			if err := a.Gateway.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("stop gateway: %w", err))
			}
		}
		a.mcpMgr.Close()
		// The runtime's optional process-resource seam (add-create-document
		// design D3): closes the document converter's headless Chrome before
		// the store goes down.
		if c, ok := a.Runtime.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close runtime: %w", err))
			}
		}
		if err := a.Store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close store: %w", err))
		}
		a.closeErr = errors.Join(errs...)
	})
	return a.closeErr
}
