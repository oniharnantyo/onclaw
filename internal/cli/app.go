package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/agents/tools/documents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/browseruse"
	"github.com/oniharnantyo/buddies/internal/channels/telegram"
	"github.com/oniharnantyo/buddies/internal/computeruse"
	"github.com/oniharnantyo/buddies/internal/config"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/promptgen"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/skills"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/sqlite"
	"github.com/oniharnantyo/buddies/internal/vault"
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

	// computer is the Computer Use lane's composition state
	// (add-computer-use design D6/D9): the pinned driver's acquisition and
	// the parent-side daemon. Close stops the daemon so nothing outlives the
	// app (spec computer-use "Driver daemon lifecycle").
	computer *computerLane

	// Services is the bound service set handed to the host.
	Services ServiceSet

	// NotificationsEngine is the notifications decision engine
	// (add-desktop-notifications design D1): the hosts reach it for the
	// click-to-open route lookup after Assemble returns — the engine is
	// built inside Assemble, so the id → route resolver cannot exist
	// earlier (the scheduleRuns fields-bound-after-construction pattern).
	NotificationsEngine *notifications.Engine

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
	Groups    *services.GroupsService
	Settings  *services.SettingsService
	Gateway   *services.GatewayService
	Schedule  *services.ScheduleService
	Knowledge *services.KnowledgeService
	Channels  *services.ChannelService
	// Notifications is the bound desktop-notification preferences service
	// (add-desktop-notifications design D5): get/save settings with the
	// engine cache refresh, plus the frontend's focus reports.
	Notifications *services.NotificationsService
}

// AssembleOption customizes assembly. The production entrypoints (the root
// command and serve) assemble with no options — the full system, the Browser
// Use system server included.
type AssembleOption func(*assembleOptions)

// assembleOptions is the mutable option state behind AssembleOption.
type assembleOptions struct {
	// systemBrowserUse spawns and supervises the Browser Use system server
	// (rebrand-browser-use-system-mcp design D6/D7).
	systemBrowserUse bool
	// systemComputerUse wires the Computer Use lane (add-computer-use design
	// D6/D9): the supervised spec, the settings hooks, and the daemon. The
	// server build's seam (computer_server.go) composes none of it.
	systemComputerUse bool
}

// WithoutSystemBrowserUse assembles without spawning the Browser Use system
// server. Tests pass it: the supervisor launches the app's own binary as the
// stdio child, and under go test that image is the test binary — re-running
// the suite — not the browser-mcp subcommand. The live browser smoke runs in
// server mode against the real binary instead.
func WithoutSystemBrowserUse() AssembleOption {
	return func(o *assembleOptions) { o.systemBrowserUse = false }
}

// WithoutSystemComputerUse assembles without the Computer Use lane
// (add-computer-use design D6): no computer-use spec registration, no
// settings hooks, no daemon. Tests pass it — same posture as
// WithoutSystemBrowserUse — and the server build's composition seam omits
// the lane regardless.
func WithoutSystemComputerUse() AssembleOption {
	return func(o *assembleOptions) { o.systemComputerUse = false }
}

// Assemble builds the full application over the data layout resolved from
// the user's home directory: it creates the layout, opens the store (the
// migration gate — a failure here is fatal to startup), and constructs the
// gateway, runtime, and services. emitter receives every service event and
// opener launches the services' system-browser flows; both must be non-nil
// (services.Noop and services.NoBrowser for hosts without those
// capabilities). notifier is the host's desktop-notification seam
// (add-desktop-notifications design D1): nil assembles the internal no-op,
// so a host without a notification surface compiles unchanged.
func Assemble(ctx context.Context, emitter services.Emitter, opener services.URLOpener, notifier Notifier, opts ...AssembleOption) (*App, error) {
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

	assembled := assembleOptions{systemBrowserUse: true, systemComputerUse: true}
	for _, opt := range opts {
		opt(&assembled)
	}

	// The Computer Use lane's composition state (add-computer-use design
	// D6/D9): the pinned driver's acquisition, the parent-side daemon, and
	// the settings hooks. Desktop wiring lives in computer_desktop.go; the
	// server build's seam (computer_server.go) composes nothing.
	computer := newComputerLane(st, layout, assembled.systemComputerUse)

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

	// The MCP connection cache backs both the runtime's per-turn tool
	// surface (WithMCPManager) and the settings service's invalidate-on-write
	// seam: an edited, disabled, or removed definition drops its cached
	// connection immediately (design D7). With WithSystemBrowserUse it also
	// spawns and supervises the Browser Use system server
	// (rebrand-browser-use-system-mcp design D6/D7): the app's own binary as
	// a stdio child rooted at the layout's browser directory, its config
	// re-read at every spawn boundary (design D8).
	var mcpOpts []mcp.ManagerOption
	if assembled.systemBrowserUse {
		mcpOpts = append(mcpOpts, mcp.WithSystemBrowserUse(layout.Browser, browserUseSystemConfig(st)))
	}
	// The computer-use system server (add-computer-use design D6): supervised
	// exactly like browser-use, opt-in by registry policy (design D2). Its
	// config provider is the permission warm-up gate — it wraps
	// ErrSystemServerHold while the lane is disabled, a permission ungranted,
	// or the pinned driver unverified, so the supervisor holds without
	// spawning (zero tools) until everything is ready. The lane-off and
	// server builds compose a no-op here.
	mcpOpts = append(mcpOpts, computer.managerOption())
	mcpMgr := mcp.NewMCPManager(mcpOpts...)
	// Boot warm-up (spec computer-use "Driver daemon lifecycle"): an enabled
	// lane with granted permissions brings the driver and the daemon up off
	// the assembly path — cosmetic to startup, logged never fatal; the
	// provider's hold covers the window until it lands.
	go computer.warmUp(ctx)
	// The hook dispatcher gates and observes every turn (spec agent-hooks):
	// resolution reads both assignment tiers fresh per turn, and both
	// handler types are enabled (design D9).
	hooksDispatcher := agents.NewHooksDispatcher(st)

	// The notifications decision engine (add-desktop-notifications design
	// D1/D4): name resolvers over the store, the host's window-visibility
	// query through the notifier seam (nil ⇒ the internal no-op with no
	// suppression query), and the settings cache loaded at boot — the
	// settings save path refreshes it (design D4). The trigger sources
	// below enqueue candidates through it; the engine owns every gate.
	notifEngine := newNotificationEngine(ctx, st, notifier)

	// The scheduler's cron brain (add-scheduler design D1). The construction
	// cycle resolves late-bound: the runtime's scheduler tool needs this
	// service, and this adapter needs the runtime, so the adapter's fields
	// are filled in below — nothing calls it before Assemble returns.
	runs := &scheduleRuns{}
	sched := scheduler.New(st, runs, gw)
	// Schedule misses and failed fires report through the Schedules
	// category (add-desktop-notifications design D2's matrix): the listener
	// re-reads the just-recorded row — ran fires report through the
	// scheduleRuns completion path instead, and roster mutations are not
	// triggers.
	sched.Subscribe(newScheduleChangeListener(notifEngine, st))

	// The two-phase group-turn wiring (add-agent-groups tasks 4.3): the ref
	// exists before the runtime so the manager can be built with
	// WithGroupTurns, and the groups service binds itself as the turn source
	// in its constructor below.
	ref := agents.NewGroupTurnRef()
	// The session service resolves the session's agent for the
	// notifications candidates below (add-desktop-notifications design D2),
	// so it is constructed before the runtime that consumes it.
	sessionSvc := sessions.NewService(st)
	runtime := agents.NewManager(st,
		agents.WithPromptDocs(agents.NewWorkspacePromptDocs(wsMgr)),
		agents.WithWorkspaces(wsMgr),
		agents.WithSkills(layout.Skills),
		agents.WithAttachments(layout.Attachments),
		agents.WithKnowledge(layout.Knowledge),
		agents.WithMCPManager(mcpMgr),
		agents.WithHooks(hooksDispatcher),
		agents.WithScheduler(sched),
		agents.WithGroupTurns(ref),
		// Turn transitions announce app-wide (add-grouped-session-rail
		// design D2): the runtime core is where UI dispatch, scheduler fire,
		// and gateway trigger converge.
		agents.WithTurnTransition(func(sessionID string, busy bool) {
			emitter.Emit(services.EventTurnsChanged, services.TurnsChangedEvent{SessionID: sessionID, Busy: busy})
		}),
		// Turn terminals and attention parks report to the notifications
		// engine (add-desktop-notifications design D2): the callbacks are
		// best-effort — a candidate build never breaks the turn path.
		agents.WithTurnFinished(func(sessionID string, origin hooks.Origin, outcome agents.TurnOutcome, turnID string) {
			enqueueTurnFinished(notifEngine, sessionSvc, sessionID, origin, outcome, turnID)
		}),
		agents.WithSessionParked(func(sessionID string, kind agents.ParkKind, interruptID, detail string) {
			enqueueSessionParked(notifEngine, sessionSvc, sessionID, kind, interruptID, detail)
		}))
	runs.runtime, runs.sessions, runs.notifEngine = runtime, sessionSvc, notifEngine
	// The minute-aligned tick loop starts on the gateway's first Running
	// transition (design D2); both hosts start the gateway after Assemble.
	sched.Attach(gw)

	// The groups service (add-agent-groups design D2-D8): the shared
	// transcript, routing, task lifecycle, and recovery over the store and
	// the runtime. Its constructor binds the runtime's group turn source
	// through ref; live updates forward onto the host emitter (design D9).
	groupSvc := groups.New(groups.Deps{
		Store:    st,
		Sessions: sessionSvc,
		Manager:  runtime,
		Ref:      ref,
		// The notifications interposition (add-desktop-notifications
		// proposal, Groups category): events forward unchanged to the UI and
		// task terminals additionally enqueue a Groups candidate.
		Events: newNotifyingGroupsEvents(services.NewGroupsEvents(emitter), notifEngine, st.Groups()),
	})
	// Restart recovery (design D8): re-drain persisted dispatches in a
	// non-fatal startup goroutine — mirroring the prompt-generation sweep's
	// posture, a failure is logged and never crashes the app.
	go func() {
		if err := groupSvc.Drain(ctx); err != nil {
			slog.Warn("assemble: group dispatch drain failed", "error", err)
		}
	}()

	agentsSvc := services.NewAgentsService(st, promptGen, wsMgr, knowStore, layout.Skills, groupSvc)
	// Sweep crash-orphaned prompt generations before services accept calls
	// (design D4): rows still in generating are moved to failed with the
	// canonical interrupted reason. Cosmetic to startup — a sweep failure
	// only leaves retryable rows stuck — so it is logged, not fatal.
	if err := agentsSvc.SweepInterruptedGenerations(ctx); err != nil {
		slog.Warn("assemble: prompt generation sweep failed", "error", err)
	}

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

	// The vault backs the Saved-logins methods and the resolve-input
	// save-to-vault path (add-browser-login-handover design D5). Its key
	// material mints lazily — only on the first save or reveal — so a
	// headless host with no keyring still boots (the keyfile fallback takes
	// over at first use).
	vaultSvc := vault.NewService(st.VaultItems(), vault.NewKeyProvider(layout.Root))

	settingsSvc := services.NewSettingsService(st,
		services.WithMCPInvalidate(mcpMgr.Invalidate),
		// The settings panes' System rows read the supervised servers' live
		// state (one row per registered system server — add-computer-use
		// design D2), and the profile-delete flow tears the supervised
		// browser-use child down before removing a profile directory
		// (rebrand-browser-use-system-mcp, spec settings "Browser Use pane").
		services.WithSystemServerState(mcpMgr.SystemServerState),
		services.WithSystemServerRestart(mcpMgr.RestartSystemServer),
		// The browser profile management roots at the layout's browser
		// directory (spec browser-use "Persistent browser profiles") — the
		// same root the system server above was given.
		services.WithBrowserDir(layout.Browser),
		// The Computer Use pane's hooks (add-computer-use design D6): the
		// permission probes, the driver's acquisition state, the
		// enable/disable/grant-change daemon kicks, and the supervised
		// adapter's live state — the lane owns the machinery, the service
		// aggregates. The lane-off and server builds compose a no-op.
		computer.settingsHooks(mcpMgr),
		// Template-document preset uploads land in the store aggregate's
		// on-disk template store (add-document-editing design D6); unwired,
		// the settings service refuses every upload.
		services.WithDocumentTemplates(st.DocumentTemplates()),
		// The MCP OAuth flow announces completion through the host's event
		// bus (design D2) and opens its authorization page through the
		// host's browser seam (design D8).
		services.WithEmitter(emitter),
		// The Browser Use pane's Saved logins section reads and writes the
		// vault (add-browser-login-handover design D5).
		services.WithVault(vaultSvc),
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

	// Seed the computer-use skill into the global skills tier when missing
	// (add-computer-use design D10, repair-if-deleted — mirroring the seeds
	// above). Cosmetic to startup — desktop work works without the playbook,
	// and agents without the server simply never activate it — so it is
	// logged, not fatal.
	if err := computeruse.SeedSkill(layout.Skills); err != nil {
		slog.Warn("assemble: computer-use skill seed failed", "error", err)
	}

	// The Channels pane's mutations kick the Telegram runtime (design D3
	// amendment: the loop re-reads its row only at (re)start, so a token
	// saved or toggle flipped after boot must trigger one — otherwise a
	// channel configured post-boot never begins polling).
	channelsSvc := services.NewChannelService(st, emitter)
	channelsSvc.OnChange = func(channelID string) { tg.ConfigChanged() }

	return &App{
		Layout:    layout,
		Store:     st,
		Gateway:   gw,
		Runtime:   runtime,
		mcpMgr:    mcpMgr,
		computer:  computer,
		sched:     sched,
		memTicker: memTicker,
		tg:        tg,
		Services: ServiceSet{
			AppInfo: services.NewAppInfo(),
			Agents:  agentsSvc,
			Chat: services.NewChatService(runtime, sessionSvc, emitter, st, attStore, wsMgr,
				// The generated-document Save As export opens the host's
				// native save dialog (add-artifact-save-as design D2); the
				// build-tag seam defaults the server build to the
				// unavailable null object, so both hosts share this wiring.
				services.WithSaveDialog(services.NewHostSaveDialog()),
				// The resolve-input save-to-vault path writes the vault
				// (add-browser-login-handover design D5).
				services.WithChatVault(vaultSvc)),
			Settings:  settingsSvc,
			Groups:    services.NewGroupsService(groupSvc),
			Gateway:   services.NewGatewayService(gw, emitter),
			Schedule:  services.NewScheduleService(sched, emitter),
			Knowledge: services.NewKnowledgeService(knowStore, st.Agents(), emitter),
			Channels:  channelsSvc,
			// The notifications service (add-desktop-notifications design
			// D5): get/save settings — the save path refreshes the engine
			// cache — and the frontend's focus reports.
			Notifications: services.NewNotificationsService(st.NotificationSettings(), notifEngine),
		},
		NotificationsEngine: notifEngine,
	}, nil
}

// browserUseSystemConfig resolves the Browser Use system server's spawn-time
// flag snapshot (rebrand-browser-use-system-mcp design D8): the persisted
// tool_settings document under the browser key — the same row the settings
// pane writes — decoded through the server package's single decode site. A
// missing row decodes to the defaults; the provider is called fresh at every
// spawn boundary and supervision tick, so a settings save applies at the
// next idle-boundary respawn without any push notification.
func browserUseSystemConfig(st store.Store) func(ctx context.Context) (mcp.SystemServerConfig, error) {
	return func(ctx context.Context) (mcp.SystemServerConfig, error) {
		raw, _, err := st.ToolSettings().Get(ctx, services.BrowserSettingsKey)
		if err != nil {
			return mcp.SystemServerConfig{}, fmt.Errorf("resolve browser-use config: %w", err)
		}
		cfg, err := browseruse.DecodeStoredConfig(raw)
		if err != nil {
			return mcp.SystemServerConfig{}, err
		}
		return mcp.SystemServerConfig{
			DefaultProfile:       cfg.DefaultProfile,
			Headless:             cfg.Headless,
			IdleTimeoutSeconds:   cfg.IdleTimeoutSeconds,
			ActionTimeoutSeconds: cfg.ActionTimeoutSeconds,
			AllowPrivate:         cfg.AllowPrivate,
		}, nil
	}
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
		// The Computer Use daemon dies with the app (add-computer-use, spec
		// computer-use "Driver daemon lifecycle": app exit leaves no orphan —
		// the graceful path here, the daemon's stdin liveness and the
		// parent-liveness watchdog as the crash paths), before the MCP
		// teardown reaps the adapter children.
		a.computer.stopAtClose()
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
