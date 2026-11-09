package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// The computer-use system server's supervision spec (add-computer-use design
// D2/D6, spec computer-use "Adapter server process"): the app spawns its own
// binary's `computer-mcp` subcommand over stdio and supervises it exactly
// like browser-use. The spec is opt-in (design D2): desktop control is
// offered only to agents whose MCPServers list names computer-use — an empty
// allowlist never silently widens to the desktop.
//
// Readiness (coordination contract C3): the config provider reports the
// permission warm-up gate's verdict — driver downloaded, permissions granted
// — by wrapping ErrSystemServerHold when any of it is pending. The
// supervisor answers a hold with the StatusHeld vocabulary: no spawn
// attempt, no failure-count movement, a re-check on every later tick.

// ComputerServerConfig is the computer-use child's spawn-time flag snapshot
// (the coordination contract's exact argv): the daemon socket, the
// computer-use data directory, and the parent's readiness verdict. A
// comparable struct of scalars, like [SystemServerConfig].
type ComputerServerConfig struct {
	// SocketPath is the driver daemon's private local socket the child
	// reaches the vendored driver through.
	SocketPath string
	// ComputerDir is the computer-use data directory (the child's fallback
	// screenshots root).
	ComputerDir string
	// Ready is the permission warm-up gate's verdict at this spawn boundary:
	// false means the child serves zero tools (and the provider normally
	// holds instead, so the child does not spawn at all — the flag is the
	// belt to the hold's suspenders).
	Ready bool
}

// ErrSystemServerHold is the typed hold a system server's config provider
// returns when the server is awaiting readiness (coordination contract C3):
// the supervisor treats it as a held state — no spawn attempt, no failure
// count, a re-check on later ticks — never as a failure. Providers wrap it
// with the reason (fmt.Errorf("%w: ...", ErrSystemServerHold)); the wrapped
// text surfaces as the held status's reason.
var ErrSystemServerHold = errors.New("system server awaiting readiness")

// WithSystemComputerUse configures the manager to spawn and supervise the
// computer-use system server (add-computer-use design D6): the computer-use
// registry spec (OptIn=true — offered only to agents naming it) with its
// config provider and ref synthesis bound. The config provider is called at
// every spawn boundary and every supervision tick (design D8); it returns
// ErrSystemServerHold-wrapped errors while the permission warm-up gate is
// pending (C3). A nil provider is ignored — without this option the manager
// behaves exactly as before.
func WithSystemComputerUse(config func(ctx context.Context) (ComputerServerConfig, error)) ManagerOption {
	if config == nil {
		return func(m *MCPManager) {}
	}
	spec, ok := registrySpec(ComputerUseServerID)
	if !ok {
		return func(m *MCPManager) {} // unreachable: the registry defines computer-use
	}
	spec.Config = func(ctx context.Context) (any, error) {
		return config(ctx)
	}
	spec.Ref = func(exe string, cfg any) Ref {
		// The hook pair above agrees on the snapshot type by construction.
		return systemComputerUseRef(exe, cfg.(ComputerServerConfig))
	}
	return WithSystemServer(spec)
}

// systemComputerUseRef synthesizes the computer-use system server's
// definition (the coordination contract's exact argv):
//
//	<own binary> computer-mcp --socket <path> --computer-dir <dir> --ready <bool>
//
// Every flag is always passed, including the boolean in explicit
// space-separated form.
func systemComputerUseRef(exe string, cfg ComputerServerConfig) Ref {
	return Ref{
		ServerID: ComputerUseServerID,
		Name:     ComputerUseServerID,
		Conn: Connection{
			Transport: TransportStdio,
			Command:   exe,
			Args: []string{
				ComputerUseSubcommand,
				"--socket", cfg.SocketPath,
				"--computer-dir", cfg.ComputerDir,
				"--ready", strconv.FormatBool(cfg.Ready),
			},
		},
	}
}

// isSystemServerHold reports whether err is (or wraps) the hold.
func isSystemServerHold(err error) bool {
	return errors.Is(err, ErrSystemServerHold)
}

// holdErrText renders a hold for the surfaced status: the provider's own
// wrapped reason when it carries one, the bare sentinel otherwise.
func holdErrText(err error) string {
	text := err.Error()
	if text == ErrSystemServerHold.Error() {
		return text
	}
	return text
}

// holdEntryErr renders the Tools-facing error of a held entry: readable and
// pointing at the warm-up, never a failure smell beyond the reason.
func holdEntryErr(id string, hold error) error {
	return fmt.Errorf("%s system server is awaiting readiness: %v", id, holdErrText(hold))
}
