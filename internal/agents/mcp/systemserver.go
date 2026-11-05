package mcp

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// The app-synthesized system MCP server (rebrand-browser-use-system-mcp,
// design D6, spec agent-mcp "System-provided server"): buddies spawns its own
// binary as a stdio MCP server (the Browser Use engine) and supervises it.
// The definition is built here, never stored as an mcp_servers row — it is
// non-deletable, non-editable, and always enabled by construction, and it
// rides the standard per-agent server allowlist (default-on).

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
// attempt), the failure reason when errored, and the discovered-tool count.
type ServerState struct {
	Status      string
	StatusError string
	ToolCount   int
}

// SystemServerSource is the optional capability of a [ToolSource] that
// provides the app-synthesized system server (design D6). The turn
// resolution type-asserts it: sources without the capability (test fakes,
// the no-op source) simply provide no system server. The returned Ref is
// supervisor-managed — its Conn carries no dial settings; [ToolSource.Tools]
// is the only valid use of it.
type SystemServerSource interface {
	// SystemServer returns the system server's Ref and whether the source
	// provides one.
	SystemServer() (Ref, bool)
}

// WithSystemBrowserUse configures the manager to spawn and supervise the
// browser-use system server (design D7): the child is spawned from the app's
// own binary at manager init and kept alive with respawn-with-backoff. The
// config provider is called at every spawn boundary so persisted settings
// changes apply to the next spawn (design D8). An empty browserDir or a nil
// provider is ignored — without this option the manager behaves exactly as
// before.
func WithSystemBrowserUse(browserDir string, config func(ctx context.Context) (SystemServerConfig, error)) ManagerOption {
	return func(m *MCPManager) {
		if browserDir == "" || config == nil {
			return
		}
		m.system = newSystemSupervisor(m, browserDir, config)
	}
}

// SystemServer implements [SystemServerSource]: the system server's Ref when
// the manager supervises one.
func (m *MCPManager) SystemServer() (Ref, bool) {
	if m.system == nil {
		return Ref{}, false
	}
	return Ref{ServerID: SystemServerID, Name: SystemServerID}, true
}

// SystemServerState reports the supervised server's live connection state
// from the shared status plumbing: the most recent spawn attempt's outcome,
// or unknown before the first attempt completes. ok=false when the manager
// supervises no system server.
func (m *MCPManager) SystemServerState() (ServerState, bool) {
	if m.system == nil {
		return ServerState{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key{SystemServerID}]
	if !ok {
		return ServerState{Status: StatusUnknown}, true
	}
	return ServerState{Status: e.status, StatusError: e.statusErr, ToolCount: e.toolCount}, true
}

// RestartSystemServer re-arms supervision after a crash-loop cap: counters
// reset, the current child (if any) is torn down, and the next supervisor
// tick respawns. A no-op when the manager supervises no system server. This
// is the manual path past the cap — the automatic reset is a successful
// spawn attempt (the probe).
func (m *MCPManager) RestartSystemServer() {
	if m.system == nil {
		return
	}
	m.system.restart()
}

// supervised reports whether serverID is the supervised system server's key.
func (m *MCPManager) supervised(serverID string) bool {
	return m.system != nil && serverID == SystemServerID
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
