package mcp

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// The app-synthesized system MCP servers (rebrand-browser-use-system-mcp
// design D6; add-computer-use design D2 generalizes the single seam to a
// registry, spec agent-mcp "System server registry"): buddies spawns its own
// binary as a stdio MCP server per registered spec and supervises it. A
// definition is built here, never stored as an mcp_servers row — it is
// non-deletable, non-editable, and always enabled by construction, and it
// rides the standard per-agent server allowlist under the registry's opt-in
// rule (design D2).

const (
	// SystemServerID is the reserved server id of the app-provided MCP
	// server. It is never a store row id: mutations addressed at it are
	// rejected (spec agent-mcp "System-provided server").
	SystemServerID = "browser-use"
	// SystemServerSubcommand launches the system server from the app's own
	// binary (design D2: same binary, subcommand entrypoint).
	SystemServerSubcommand = "browser-mcp"
	// SystemMCPServerName is the system server's display name. It also names
	// the server in the tool naming pass, so tools surface as
	// mcp__browser-use__<tool> — the name sanitizes to itself.
	SystemMCPServerName = "Browser Use"
)

// ComputerUseServerID and ComputerUseSubcommand are the computer-use system
// server's reserved id and launch subcommand (add-computer-use coordination
// contract; systemserver_computer.go carries the spec): the tool namespace
// segment mcp__computer-use__<tool> and the `computer-mcp` subcommand of
// the app's own binary.
const (
	ComputerUseServerID   = "computer-use"
	ComputerUseSubcommand = "computer-mcp"
	SystemComputerUseName = "Computer Use"
)

// SystemServerConfig is the browser-use child's spawn-time flag snapshot
// (design D8): the values the supervisor passes to the subcommand when it
// spawns (or respawns) the child. The provider supplies it fresh at every
// spawn boundary, so settings changes take effect at the next spawn without
// ever restarting a live child mid-action.
type SystemServerConfig struct {
	// DefaultProfile is the profile the child's actions run in when the
	// model passes none. Resolved by the caller — empty means the child's
	// own default.
	DefaultProfile string
	// Headless launches Chromium without a window.
	Headless bool
	// IdleTimeoutSeconds closes a profile's idle browser after this long.
	IdleTimeoutSeconds int
	// ActionTimeoutSeconds bounds each browser action.
	ActionTimeoutSeconds int
	// AllowPrivate permits navigation to loopback/private network targets.
	AllowPrivate bool
}

// ServerState is one server's live connection view: the status vocabulary of
// the probe (StatusConnected / StatusError / "unknown" before the first
// attempt), the failure reason when errored, the discovered-tool count, and
// whether supervision has hit its crash-loop cap — respawning stopped, so
// nothing will recover the server automatically until an explicit restart
// (the settings service's on-demand probe re-arms it, design D2's manual
// path past the cap).
type ServerState struct {
	Status      string
	StatusError string
	ToolCount   int
	// Capped marks supervision stopped after its bounded consecutive
	// failures: the errored state below is final until a restart re-arms
	// supervision (or a successful spawn attempt, which cannot happen while
	// capped).
	Capped bool
}

// SystemServerSpec is one built-in system server's definition in the
// registry (add-computer-use design D2): identity and policy fields, plus
// the runtime hooks supervision needs — the config provider called fresh at
// every spawn boundary (design D8 semantics, per server) and the config→Ref
// synthesis rendering the coordination contract's exact argv. Identity and
// policy are static (see systemServerRegistry); the hooks are bound by the
// spec's WithSystemServer* option at composition time.
type SystemServerSpec struct {
	// ID is the server's reserved id: the cache key, the allowlist name an
	// agent's MCPServers list writes, and the tool namespace segment
	// (mcp__<ID>__<tool> — the synthesized Ref's Name is the ID so the name
	// sanitizes to itself).
	ID string
	// Subcommand launches the server from the app's own binary (design D2:
	// same binary, subcommand entrypoint). The Ref synthesis renders it as
	// argv[0].
	Subcommand string
	// DisplayName is the server's display name — the settings pane's System
	// row label. The tool naming pass uses the ID, never this name.
	DisplayName string
	// OptIn marks the server opt-in (add-computer-use design D2): an opt-in
	// system server is offered only to agents whose MCPServers list names
	// its ID — withheld even when an agent's list is empty — so registering
	// one never silently widens agents whose allowlist is empty. The default
	// (false) keeps the offered-to-every-agent-by-default posture.
	OptIn bool
	// Config supplies the server's spawn-time configuration snapshot,
	// fetched fresh at every spawn attempt and every supervision tick
	// (design D8): a difference against the snapshot the current child was
	// spawned with schedules an idle-boundary restart. The snapshot is
	// opaque to the supervisor — a comparable value type (a struct of
	// scalars) is the intended shape; equality is compared deeply so an
	// exotic shape degrades to change detection, never a panic.
	Config func(ctx context.Context) (any, error)
	// Ref synthesizes the server's definition from the app's own executable
	// path and a snapshot Config returned — the coordination contract's
	// exact argv. Ref receives only snapshots Config produced, so the hook
	// pair agrees on the snapshot type by construction.
	Ref func(exe string, cfg any) Ref
}

// systemServerRegistry is the static side of the registry (add-computer-use
// design D2): every built-in system server's identity and policy, in
// definition order. The runtime hooks live on the full specs the
// WithSystemServer* options bind; this slice is the source of truth
// consumers enumerate without a manager — the settings service's reserved-id
// guard rejects mutations addressed at any id here. Adding a system server
// is a pure addition: one entry here plus its supervision option.
var systemServerRegistry = []SystemServerSpec{
	{
		ID:          SystemServerID,
		Subcommand:  SystemServerSubcommand,
		DisplayName: SystemMCPServerName,
		OptIn:       false, // Browser Use stays offered to every agent by default (design D2)
	},
	{
		ID:          ComputerUseServerID,
		Subcommand:  ComputerUseSubcommand,
		DisplayName: SystemComputerUseName,
		// OptIn (add-computer-use design D2): desktop control is offered
		// only to agents whose allowlist names it — an empty allowlist never
		// silently widens to the desktop.
		OptIn: true,
	},
}

// registrySpec returns a copy of the static registry entry with the given
// id, so a supervision option binds its hooks onto the same identity and
// policy the reserved-id guard enumerates (add-computer-use design D2: one
// source of truth). ok=false for an unregistered id.
func registrySpec(id string) (SystemServerSpec, bool) {
	for _, spec := range systemServerRegistry {
		if spec.ID == id {
			return spec, true
		}
	}
	return SystemServerSpec{}, false
}

// SystemServerIDs returns every registered system server's id in registry
// order (add-computer-use design D2): the reserved ids no store row may
// carry — the settings service rejects every mutation addressed at one of
// them (spec agent-mcp "System-provided server": non-deletable,
// non-editable by construction). Static per binary: a spec's runtime hooks
// are bound at composition time, but its identity is defined here.
func SystemServerIDs() []string {
	ids := make([]string, 0, len(systemServerRegistry))
	for _, spec := range systemServerRegistry {
		ids = append(ids, spec.ID)
	}
	return ids
}

// SystemServerDisplayName returns the registered system server's display
// name (add-computer-use design D2): the settings service synthesizes each
// system row's Name from it, so a new registry entry renders under its own
// name with no settings-side change. ok=false for an unregistered id.
func SystemServerDisplayName(id string) (string, bool) {
	spec, ok := registrySpec(id)
	if !ok {
		return "", false
	}
	return spec.DisplayName, true
}

// SystemServerEntry is one supervised system server's registry view
// (add-computer-use design D2): the Ref the merge point resolves the
// server's tools through and the spec's opt-in flag the allowlist rule
// reads.
type SystemServerEntry struct {
	// Ref is the supervisor-managed system server's definition — its Conn
	// carries no dial settings; [ToolSource.Tools] is the only valid use of
	// it. Name is the spec's ID so tools surface as mcp__<ID>__<tool>.
	Ref Ref
	// OptIn carries the spec's opt-in policy (add-computer-use design D2):
	// an opt-in server is offered only to agents whose MCPServers list names
	// Ref.ServerID, even when the list is empty.
	OptIn bool
}

// SystemServerSource is the optional capability of a [ToolSource] that
// provides the app-synthesized system servers (add-computer-use design D2;
// formerly the single-server SystemServer seam). The turn resolution
// type-asserts it: sources without the capability (test fakes, the no-op
// source) simply provide no system servers. Every returned Ref is
// supervisor-managed — its Conn carries no dial settings; [ToolSource.Tools]
// is the only valid use of it.
type SystemServerSource interface {
	// SystemServers returns the registry, one entry per supervised system
	// server in registration order.
	SystemServers() []SystemServerEntry
}

// WithSystemServer registers one system server spec for supervision
// (add-computer-use design D2): the child is spawned from the app's own
// binary at manager init and kept alive with respawn-with-backoff, keyed by
// the spec's ID in the shared connection cache. The config provider is
// called at every spawn boundary so persisted settings changes apply to the
// next spawn (design D8). The option accepts arbitrary specs — identity need
// not appear in the static registry. An invalid spec (empty ID, or a nil
// Config/Ref hook) and a duplicate registration of an id the manager already
// supervises are ignored: without a valid new spec the manager behaves
// exactly as before.
func WithSystemServer(spec SystemServerSpec) ManagerOption {
	return func(m *MCPManager) {
		if spec.ID == "" || spec.Config == nil || spec.Ref == nil {
			return
		}
		if _, dup := m.systemSupervisor(spec.ID); dup {
			return
		}
		m.systems = append(m.systems, newSystemSupervisor(m, spec))
	}
}

// WithSystemBrowserUse configures the manager to spawn and supervise the
// browser-use system server (design D7): the browser-use registry spec
// (OptIn=false — offered to every agent by default, bit-identical to the
// pre-registry seam) with its config provider and ref synthesis bound to the
// layout's browser directory. The child is spawned from the app's own binary
// at manager init and kept alive with respawn-with-backoff. The config
// provider is called at every spawn boundary so persisted settings changes
// apply to the next spawn (design D8). An empty browserDir or a nil provider
// is ignored — without this option the manager behaves exactly as before.
func WithSystemBrowserUse(browserDir string, config func(ctx context.Context) (SystemServerConfig, error)) ManagerOption {
	if browserDir == "" || config == nil {
		return func(m *MCPManager) {}
	}
	spec, ok := registrySpec(SystemServerID)
	if !ok {
		return func(m *MCPManager) {} // unreachable: the registry defines browser-use
	}
	spec.Config = func(ctx context.Context) (any, error) {
		return config(ctx)
	}
	spec.Ref = func(exe string, cfg any) Ref {
		// The hook pair above agrees on the snapshot type by construction.
		return systemBrowserUseRef(exe, browserDir, cfg.(SystemServerConfig))
	}
	return WithSystemServer(spec)
}

// SystemServers implements [SystemServerSource]: one entry per supervised
// system server, in registration order (add-computer-use design D2). The
// merge point iterates it and applies the opt-in rule per entry.
func (m *MCPManager) SystemServers() []SystemServerEntry {
	if len(m.systems) == 0 {
		return nil
	}
	entries := make([]SystemServerEntry, 0, len(m.systems))
	for _, s := range m.systems {
		entries = append(entries, SystemServerEntry{
			Ref:   Ref{ServerID: s.spec.ID, Name: s.spec.ID},
			OptIn: s.spec.OptIn,
		})
	}
	return entries
}

// SystemServerState reports the supervised server's live connection state
// from the shared status plumbing: the most recent spawn attempt's outcome,
// or unknown before the first attempt completes. Capped reports whether
// supervision has stopped respawning after its crash-loop cap — recoverable
// only by an explicit restart (RestartSystemServer, or the settings probe's
// re-arm). ok=false when the manager supervises no system server with the
// given id (add-computer-use design D2: supervision state is keyed by spec
// ID).
func (m *MCPManager) SystemServerState(id string) (ServerState, bool) {
	s, ok := m.systemSupervisor(id)
	if !ok {
		return ServerState{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key{id}]
	if !ok {
		return ServerState{Status: StatusUnknown, Capped: s.capped}, true
	}
	return ServerState{Status: e.status, StatusError: e.statusErr, ToolCount: e.toolCount, Capped: s.capped}, true
}

// RestartSystemServer re-arms supervision after a crash-loop cap: counters
// reset, the current child (if any) is torn down, and the next supervisor
// tick respawns. A no-op when the manager supervises no system server with
// the given id. This is the manual path past the cap — the automatic reset
// is a successful spawn attempt (the probe).
func (m *MCPManager) RestartSystemServer(id string) {
	if s, ok := m.systemSupervisor(id); ok {
		s.restart()
	}
}

// systemSupervisor returns the supervisor owning the spec with the given id
// (add-computer-use design D2: one supervisor per registered spec, keyed by
// the spec's ID). ok=false when the manager supervises none with that id.
// m.systems is written only while the manager is being configured, so the
// read needs no lock.
func (m *MCPManager) systemSupervisor(id string) (*systemSupervisor, bool) {
	for _, s := range m.systems {
		if s.spec.ID == id {
			return s, true
		}
	}
	return nil, false
}

// supervised reports whether serverID is a supervised system server's cache
// key (add-computer-use design D2: one key per spec ID).
func (m *MCPManager) supervised(serverID string) bool {
	_, ok := m.systemSupervisor(serverID)
	return ok
}

// systemBrowserUseRef synthesizes the system server's definition (design D6):
// stdio, the app's own binary, the browser-mcp subcommand, and the config
// snapshot as explicit flags — the coordination contract's exact argv:
//
//	<own binary> browser-mcp --browser-dir <path> --default-profile <name>
//	    --headless <bool> --idle-timeout <secs> --action-timeout <secs>
//	    --allow-private <bool>
//
// Every flag is always passed, including boolean values in explicit
// space-separated form. The private-network env passthrough
// (BUDDIES_BROWSER_ALLOW_PRIVATE) rides the child-env baseline, not argv.
func systemBrowserUseRef(exe, browserDir string, cfg SystemServerConfig) Ref {
	return Ref{
		ServerID: SystemServerID,
		Name:     SystemServerID,
		Conn: Connection{
			Transport: TransportStdio,
			Command:   exe,
			Args: []string{
				SystemServerSubcommand,
				"--browser-dir", browserDir,
				"--default-profile", cfg.DefaultProfile,
				"--headless", strconv.FormatBool(cfg.Headless),
				"--idle-timeout", strconv.Itoa(cfg.IdleTimeoutSeconds),
				"--action-timeout", strconv.Itoa(cfg.ActionTimeoutSeconds),
				"--allow-private", strconv.FormatBool(cfg.AllowPrivate),
			},
		},
	}
}

// ownExecutable resolves the app's own binary path (design D2: the system
// server is this process's image re-invoked as a subcommand).
func ownExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve own binary: %w", err)
	}
	return exe, nil
}
