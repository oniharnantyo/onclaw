package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/oniharnantyo/buddies/internal/computeruse"
	"github.com/oniharnantyo/buddies/internal/config"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The Computer Use lane's composition-root state (add-computer-use design
// D6/D9, tasks 2.6 + 3.1's wiring half): the desktop composition owns the
// parent-side driver machinery — the pinned driver's acquisition state and
// the daemon child — and supervises the adapter through the MCP manager's
// computer-use spec. The server build compiles the type but wires nothing:
// the composition methods are the build-tag seam (computer_desktop.go /
// computer_server.go, the clipboard service's seam pattern), so server mode
// has no spec, no daemon, and no hooks (spec computer-use "Desktop-only
// availability", design D9).
type computerLane struct {
	st     store.Store
	layout config.Layout
	// enabled is the composition's lane switch: WithoutSystemComputerUse
	// (tests) and the server seam both compose with it off — every
	// composition method then answers a no-op without touching the driver.
	enabled bool

	// ensureMu single-flights the pinned driver's acquisition: the
	// supervisor's spawn ticks, the settings hooks, and the boot warm-up all
	// funnel through one fetch, and a boundary that finds one in flight
	// answers not-ready instead of waiting (driverGate's TryLock).
	ensureMu sync.Mutex
	// ensuring reports an acquisition in flight — the pane's "downloading"
	// driver state (read without the pin mutex so status reads never block
	// on a fetch).
	ensuring atomic.Bool
	// pinMu guards the last acquisition snapshot and its stat signature.
	pinMu  sync.Mutex
	pin    computeruse.PinStatus
	pinSig string

	// daemonMu guards the parent-side daemon manager; a pin change (re-
	// download, version bump) replaces it outright.
	daemonMu sync.Mutex
	daemon   *computeruse.Manager
	binPath  string

	// warmStop cancels the daemon reconciliation loop (warmUp's); stopAtClose
	// cancels it so the loop never respawns a daemon into a closing app.
	warmMu   sync.Mutex
	warmStop context.CancelFunc
	// closing flags the app's graceful close for a reconcile tick already in
	// flight when warmStop lands (the loop must not respawn past teardown).
	closing atomic.Bool
}

// newComputerLane builds the lane over the resolved layout and store.
func newComputerLane(st store.Store, layout config.Layout, enabled bool) *computerLane {
	return &computerLane{st: st, layout: layout, enabled: enabled}
}

// storedConfig reads the persisted tool_settings "computer" document — the
// same row the settings pane writes, decoded through computeruse (the single
// decode site). A missing row decodes to the default (disabled): Computer
// Use is opt-in end to end (add-computer-use design D2).
func (l *computerLane) storedConfig(ctx context.Context) (computeruse.Config, error) {
	raw, _, err := l.st.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	if err != nil {
		return computeruse.Config{}, fmt.Errorf("resolve computer-use config: %w", err)
	}
	return computeruse.DecodeStoredConfig(raw)
}

// computerDir is the computer-use data directory (add-computer-use design
// D1): under the layout root beside browser/ — the daemon socket lives
// inside it and the child's fallback screenshots root is screenshots/ under
// it. Per-session screenshot targets ride the call's _meta parent-side (the
// browser-use pattern's per-call session context); this directory is only
// the fallback the child owns.
func (l *computerLane) computerDir() string {
	return filepath.Join(l.layout.Root, "computer")
}

// socketPath is the daemon's private local socket (the driver package's
// convention: inside the computer-use data directory) — the adapter child's
// --socket value.
func (l *computerLane) socketPath() string {
	return filepath.Join(l.computerDir(), computeruse.SocketFileName)
}
