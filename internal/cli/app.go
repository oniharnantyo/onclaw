package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/config"
	"github.com/oniharnantyo/buddies/internal/gateway"
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

	closeOnce sync.Once
	closeErr  error
}

// ServiceSet is the set of services bound to the host's frontend.
type ServiceSet struct {
	AppInfo  *services.AppInfo
	Agents   *services.AgentsService
	Chat     *services.ChatService
	Settings *services.SettingsService
	Gateway  *services.GatewayService
	Schedule *services.ScheduleService
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
	agentsSvc := services.NewAgentsService(st, promptGen, wsMgr)

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
		agents.WithBrowserDir(layout.Browser),
		agents.WithMCPManager(mcpMgr),
		agents.WithHooks(hooksDispatcher),
		agents.WithScheduler(sched))
	sessionSvc := sessions.NewService(st)
	runs.runtime, runs.sessions = runtime, sessionSvc
	// The minute-aligned tick loop starts on the gateway's first Running
	// transition (design D2); both hosts start the gateway after Assemble.
	sched.Attach(gw)

	// The capability-gated attachment store (design D5) serves the chat
	// attachments feature from the layout's attachments directory.
	attStore := attachments.NewStore(layout.Attachments, st.Attachments())

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

	return &App{
		Layout:  layout,
		Store:   st,
		Gateway: gw,
		Runtime: runtime,
		mcpMgr:  mcpMgr,
		sched:   sched,
		Services: ServiceSet{
			AppInfo: services.NewAppInfo(),
			Agents:  agentsSvc,
			Chat: services.NewChatService(runtime, sessionSvc, emitter, st, attStore, wsMgr,
				// The browser-screenshot read serves from the layout's
				// browser directory (design D6) — the same root the
				// runtime's browser tool was given above.
				services.WithChatBrowserDir(layout.Browser)),
			Settings: settingsSvc,
			Gateway:  services.NewGatewayService(gw, emitter),
			Schedule: services.NewScheduleService(sched, emitter),
		},
	}, nil
}

// Close shuts the application down: it stops the scheduler's tick loop (so
// no tick races the teardown below), stops the gateway (unless already
// stopped), tears down the MCP connection cache (reaping any stdio
// children), and closes the store. It is idempotent; the first call's result
// is returned on every call.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		var errs []error
		a.sched.Stop()
		if a.Gateway.State() != gateway.StateStopped {
			if err := a.Gateway.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("stop gateway: %w", err))
			}
		}
		a.mcpMgr.Close()
		if err := a.Store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close store: %w", err))
		}
		a.closeErr = errors.Join(errs...)
	})
	return a.closeErr
}
