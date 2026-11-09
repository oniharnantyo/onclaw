//go:build !server

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/computeruse"
	"github.com/oniharnantyo/buddies/internal/services"
)

// The desktop Computer Use composition (add-computer-use design D6/D9, tasks
// 2.6 + 3.1's wiring half): the lane is wired whole — the supervised
// computer-use spec with its warm-up-gated config provider, the settings
// service's hooks, and the boot warm-up. Server mode compiles the
// computer_server.go stubs instead (spec computer-use "Desktop-only
// availability").

// computerDriverFetchTimeout bounds one pinned-driver acquisition: a stalled
// release fetch must never pin a supervision tick or a settings call
// indefinitely (the acquire answers a readable hold, and the next boundary
// re-checks).
const computerDriverFetchTimeout = 5 * time.Minute

// managerOption returns the computer-use system server's supervision option
// (design D6: WithSystemComputerUse mirrors WithSystemBrowserUse — the app
// spawns and supervises its own `computer-mcp` subcommand). The lane off
// (WithoutSystemComputerUse) composes a no-op: the manager behaves exactly
// as before.
func (l *computerLane) managerOption() mcp.ManagerOption {
	if !l.enabled {
		return func(*mcp.MCPManager) {}
	}
	return mcp.WithSystemComputerUse(l.spawnConfig)
}

// spawnConfig is the config provider the supervisor calls fresh at every
// spawn boundary and supervision tick (the coordination contract's C3). It
// is the permission warm-up gate (add-computer-use design D6, spec
// computer-use "Permission warm-up gate"): while the stored lane is
// disabled, any OS permission is ungranted, or the pinned driver is not
// downloaded and verified, it wraps mcp.ErrSystemServerHold — the supervisor
// holds without spawning (status "held", zero tools, no failure-count
// movement) and re-checks on the next tick. A ready answer carries the
// daemon socket and the computer data directory.
func (l *computerLane) spawnConfig(ctx context.Context) (mcp.ComputerServerConfig, error) {
	cfg, err := l.storedConfig(ctx)
	if err != nil {
		return mcp.ComputerServerConfig{}, err
	}
	if !cfg.Enabled {
		return mcp.ComputerServerConfig{}, fmt.Errorf("%w: computer use is disabled", mcp.ErrSystemServerHold)
	}
	// The probe never raises a prompt (design D6: the app owns prompting;
	// the driver never prompts) — an ungranted permission holds the lane
	// with zero tools until the grant lands.
	for _, p := range services.ComputerPermissionProbe() {
		if !p.Granted {
			return mcp.ComputerServerConfig{}, fmt.Errorf("%w: %s permission is not granted", mcp.ErrSystemServerHold, p.Name)
		}
	}
	pin, ready := l.driverPin(ctx)
	if !ready {
		return mcp.ComputerServerConfig{}, fmt.Errorf("%w: driver: %s", mcp.ErrSystemServerHold, computerPinReason(pin))
	}
	return mcp.ComputerServerConfig{
		SocketPath:  l.socketPath(),
		ComputerDir: l.computerDir(),
		Ready:       true,
	}, nil
}

// driverPin answers the driver-readiness gate (spec computer-use "Driver
// acquisition and integrity": a digest mismatch refuses to run). A verified
// install is memoized and re-checked by stat (size + mtime) — the supervisor
// ticks twice a second and a full SHA-256 re-verification per tick is not
// tick-cheap; any change (or a vanished binary) falls through to the full
// idempotent ensure, so an enabled lane self-heals onto the pinned release.
// The acquisition is single-flight: a boundary that finds one in flight
// answers not-ready and lets the next tick re-check, never queueing behind
// the fetch.
func (l *computerLane) driverPin(ctx context.Context) (computeruse.PinStatus, bool) {
	l.pinMu.Lock()
	pin, sig := l.pin, l.pinSig
	l.pinMu.Unlock()
	if pin.Ready && sig != "" {
		if info, err := os.Stat(pin.BinaryPath); err == nil && statSignature(info) == sig {
			return pin, true
		}
	}
	if !l.ensureMu.TryLock() {
		return computeruse.PinStatus{Reason: "a pinned-driver fetch is already in flight"}, false
	}
	defer l.ensureMu.Unlock()
	fetchCtx, cancel := context.WithTimeout(ctx, computerDriverFetchTimeout)
	defer cancel()
	l.ensuring.Store(true)
	defer l.ensuring.Store(false)
	pin = computeruse.EnsureDriverDownloaded(fetchCtx, computeruse.DriversRoot())
	l.setPin(pin)
	return pin, pin.Ready
}

// setPin records an acquisition snapshot; the stat signature memoizes the
// verified binary between boundaries (the digest itself was verified by the
// ensure — the memo re-checks that the very same file is still on disk).
func (l *computerLane) setPin(pin computeruse.PinStatus) {
	sig := ""
	if pin.Ready && pin.BinaryPath != "" {
		if info, err := os.Stat(pin.BinaryPath); err == nil {
			sig = statSignature(info)
		}
	}
	l.pinMu.Lock()
	l.pin, l.pinSig = pin, sig
	l.pinMu.Unlock()
}

// statSignature identifies a file by its size and mtime: the cheap
// change-detection behind the memoized verified pin.
func statSignature(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// computerPinReason renders a not-ready pin for a readable hold or error:
// the acquisition's failure class when it carries one, a neutral pending
// text otherwise.
func computerPinReason(pin computeruse.PinStatus) string {
	if pin.Reason != "" {
		return pin.Reason
	}
	return "the pinned driver is not acquired yet"
}

// ensure acquires the pinned driver if needed and starts the daemon (the
// enable hook's kick and the boot warm-up's path; spec computer-use "Driver
// daemon lifecycle": a direct child of the app on a private socket, started
// only when the warm-up gate allows). The daemon start is idempotent.
func (l *computerLane) ensure(ctx context.Context) error {
	pin, ready := l.driverPin(ctx)
	if !ready {
		return fmt.Errorf("computer driver: %s", computerPinReason(pin))
	}
	l.daemonMu.Lock()
	defer l.daemonMu.Unlock()
	return l.daemonManager(pin.BinaryPath).Start(ctx)
}

// restart is the grant-change path (spec computer-use "Grant change
// restarts the daemon": permission answers are cached per process): stop and
// start the daemon at what is by definition an idle boundary, re-resolving
// the driver first so a pin bump rides along.
func (l *computerLane) restart(ctx context.Context) error {
	pin, ready := l.driverPin(ctx)
	if !ready {
		return fmt.Errorf("computer driver: %s", computerPinReason(pin))
	}
	l.daemonMu.Lock()
	defer l.daemonMu.Unlock()
	return l.daemonManager(pin.BinaryPath).Restart(ctx)
}

// stop tears the daemon down (the disable path, spec computer-use
// "Configuration and spawn boundaries": no tools are offered from the next
// turn on). Stopping a never-started lane is a no-op.
func (l *computerLane) stop(_ context.Context) error {
	l.daemonMu.Lock()
	defer l.daemonMu.Unlock()
	if l.daemon == nil {
		return nil
	}
	return l.daemon.Stop()
}

// daemonManager returns the lane's daemon manager, rebuilt for the verified
// binary when the pin changed (a re-download or version bump replaces it
// outright). The caller holds daemonMu.
func (l *computerLane) daemonManager(binPath string) *computeruse.Manager {
	if l.daemon != nil && l.binPath == binPath {
		return l.daemon
	}
	l.daemon = computeruse.NewManager(binPath, l.computerDir())
	l.binPath = binPath
	return l.daemon
}

// driverState adapts the lane's acquisition snapshot into the pane's driver
// state (design D6: download state + verification outcome — the pin's
// reason names only the failure class, never a digest).
func (l *computerLane) driverState() services.ComputerDriverState {
	if l.ensuring.Load() {
		return services.ComputerDriverState{State: services.ComputerDriverDownloading}
	}
	l.pinMu.Lock()
	pin := l.pin
	l.pinMu.Unlock()
	switch {
	case pin.Ready:
		return services.ComputerDriverState{State: services.ComputerDriverReady}
	case pin.Reason != "":
		return services.ComputerDriverState{State: services.ComputerDriverFailed, Detail: pin.Reason}
	default:
		return services.ComputerDriverState{State: services.ComputerDriverPending}
	}
}

// settingsHooks binds the settings service's Computer Use hooks onto the
// lane's machinery (design D6's WithComputerHooks seam): the pane's status
// aggregation and the enable/disable/grant-change kicks all resolve here.
// SupervisionState reads the supervised computer-use adapter's live state
// keyed by its registry id (design D2).
func (l *computerLane) settingsHooks(mgr *mcp.MCPManager) services.SettingsOption {
	if !l.enabled {
		return func(*services.SettingsService) {}
	}
	return services.WithComputerHooks(services.ComputerHooks{
		PinStatus: l.driverState,
		ProbePermissions: func() ([]services.ComputerPermission, error) {
			return services.ComputerPermissionProbe(), nil
		},
		PromptPermissions: services.ComputerPermissionPrompt,
		EnsureDriver:      l.ensure,
		RestartDriver:     l.restart,
		StopDriver:        l.stop,
		SupervisionState: func() mcp.ServerState {
			state, _ := mgr.SystemServerState(mcp.ComputerUseServerID)
			return state
		},
		DaemonRunning: l.daemonRunning,
	})
}

// computerWarmTick is the daemon reconciliation interval: a boot probe that
// raced the launch (a Quit & Reopen binds grants mid-launch, so warm-up's
// first look can answer ungranted) and a daemon that died later in the
// session are both healed within one tick. The supervisor heals the ADAPTER
// on crash; before this loop, nothing healed the DAEMON — a stale boot probe
// left the lane adapter-up but daemon-less, and every tool call failed with
// a transport handshake error until the app restarted.
const computerWarmTick = 5 * time.Second

// warmUp runs the lane's daemon reconciliation loop (spec computer-use
// "Driver daemon lifecycle"): while the app lives, every tick brings the
// daemon up when the warm-up gate answers ready but none is running, and
// otherwise waits — the boot race and a mid-session daemon crash both
// self-heal. Cosmetic by construction: an idle lane's tick short-circuits
// on the daemon's liveness before any store read, and a hold is logged
// never fatal (the spawn provider's hold covers the adapter meanwhile).
func (l *computerLane) warmUp(_ context.Context) {
	if !l.enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.warmMu.Lock()
	if l.warmStop != nil {
		l.warmStop()
	}
	l.warmStop = cancel
	l.warmMu.Unlock()
	defer cancel()
	ticker := time.NewTicker(computerWarmTick)
	defer ticker.Stop()
	for {
		if !l.closing.Load() {
			l.reconcile(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reconcile brings the daemon up exactly when the warm-up gate (spawnConfig,
// the single gate definition — contract C3) answers ready and no daemon is
// running. A running daemon short-circuits before any read; a hold falls
// through silently — the next tick re-checks.
func (l *computerLane) reconcile(ctx context.Context) {
	if l.daemonRunning() {
		return
	}
	if _, err := l.spawnConfig(ctx); err != nil {
		return // the hold is the gate's answer; the next tick re-checks
	}
	if err := l.ensure(ctx); err != nil {
		slog.Warn("computer: daemon reconcile skipped", "error", err)
	}
}

// daemonRunning reports whether the lane's daemon child is up — a dead
// generation counts as down (computeruse.Manager.Running reads the process,
// not the memory of one).
func (l *computerLane) daemonRunning() bool {
	l.daemonMu.Lock()
	defer l.daemonMu.Unlock()
	return l.daemon != nil && l.daemon.Running()
}

// stopAtClose tears the daemon down on the app's graceful close (spec
// computer-use "Driver daemon lifecycle": the daemon never outlives the app
// — the crash paths are the daemon's own stdin liveness and the
// parent-liveness watchdog; this is the graceful one). The reconciliation
// loop stops first and is flagged, so a tick in flight cannot respawn a
// daemon into the teardown.
func (l *computerLane) stopAtClose() {
	l.closing.Store(true)
	l.warmMu.Lock()
	cancel := l.warmStop
	l.warmMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if err := l.stop(context.Background()); err != nil {
		slog.Warn("close: stop computer driver daemon", "error", err)
	}
}
