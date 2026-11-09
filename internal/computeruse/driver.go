package computeruse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// The parent-side driver daemon lifecycle (add-computer-use design D1, spec
// computer-use "Driver daemon lifecycle"): the app spawns `cua-driver serve
// --embedded --socket <path>` as a DIRECT child of itself — never through a
// system launcher, so macOS TCC attributes grants to buddies and the vendor
// signature rides untouched. The daemon's telemetry and update checks are
// disabled by the exact environment contract below (the _RS_ infix names are
// the driver's; DO_NOT_TRACK is the belt to the suspenders). Liveness is
// layered: the daemon watches its own stdin (CUA_DRIVER_PARENT_LIVENESS_STDIN=1,
// the parent holds the write end and closes it on teardown), the process
// group dies with the app's crash paths (Pdeathsig on Linux, kill-on-close
// Job Object on Windows), and a parent-liveness watchdog force-kills the
// group as the last resort — the daemon never outlives the app.

const (
	// SocketFileName is the daemon's private local socket inside the
	// computer-use data directory.
	SocketFileName = "cua-driver.sock"
	// socketWaitTimeout bounds the post-spawn wait for the socket to appear.
	socketWaitTimeout = 15 * time.Second
	// socketWaitPoll is the socket-existence poll interval.
	socketWaitPoll = 100 * time.Millisecond
	// terminateGrace is how long SIGTERM teardown waits before SIGKILL.
	terminateGrace = 3 * time.Second
	// watchdogInterval is how often the parent-liveness watchdog polls.
	watchdogInterval = 2 * time.Second
)

// driverEnv is the exact environment the daemon is spawned with (design D1):
// telemetry off, update checks off, embedded mode on, stdin liveness on, and
// DO_NOT_TRACK over both. Deliberately nothing else — no parent variable
// leaks into the driver.
var driverEnv = []string{
	"DO_NOT_TRACK=1",
	"CUA_DRIVER_RS_TELEMETRY_ENABLED=0",
	"CUA_DRIVER_RS_UPDATE_CHECK=0",
	"CUA_DRIVER_EMBEDDED=1",
	"CUA_DRIVER_PARENT_LIVENESS_STDIN=1",
}

// Manager owns the driver daemon child for the app process: Start (idempotent),
// Restart (the grant-change hook — permission answers are cached per process,
// so the daemon must restart after a grant flips), and Stop. The zero value
// is not usable; build one with NewManager.
type Manager struct {
	mu          sync.Mutex
	binPath     string
	computerDir string
	proc        *daemonProc
}

// NewManager builds the daemon manager for the verified pinned binary and
// the computer-use data directory (the socket's parent).
func NewManager(binPath, computerDir string) *Manager {
	return &Manager{binPath: binPath, computerDir: computerDir}
}

// SocketPath reports the daemon's socket path (the adapter's --socket value).
func (m *Manager) SocketPath() string {
	return filepath.Join(m.computerDir, SocketFileName)
}

// Running reports whether a daemon child is currently up — a generation
// that already exited counts as down (its reaper reaps; Start spawns the
// next one). This is what makes the lane's reconciliation able to heal a
// crashed daemon instead of trusting a remembered spawn.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.proc != nil && !m.proc.hasExited()
}

// Start spawns the daemon as a direct child, waits for its socket, and
// leaves it running. Idempotent: a running daemon is left untouched (design
// D8: changes apply at spawn boundaries, never mid-action); a generation
// that already exited is replaced.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil {
		if !m.proc.hasExited() {
			return nil
		}
		// A dead generation is as good as none — the socket it left behind
		// is cleared by the fresh spawn below.
		m.proc = nil
	}
	if m.binPath == "" {
		return errors.New("computer: driver binary is not resolved; download the pinned release first")
	}
	if _, err := os.Stat(m.binPath); err != nil {
		return fmt.Errorf("computer: driver binary: %w", err)
	}
	if err := os.MkdirAll(m.computerDir, 0o700); err != nil {
		return fmt.Errorf("computer: create computer dir: %w", err)
	}
	socketPath := m.SocketPath()
	// A dead daemon leaves its socket file behind; a fresh spawn must not
	// inherit it (the driver binds exclusively).
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("computer: clear stale socket: %w", err)
	}

	// The liveness pipe: the parent holds the write end (closed at teardown),
	// the daemon reads EOF from the read end when the parent goes away.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("computer: create liveness pipe: %w", err)
	}

	cmd := exec.Command(m.binPath, "serve", "--embedded", "--socket", socketPath)
	cmd.Env = driverEnv
	cmd.Stdin = stdinR
	// The daemon's logs route to the app's stderr, which supervision surfaces
	// as failure excerpts — never into any MCP transport.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	group, err := startProcGroup(cmd)
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return fmt.Errorf("computer: start driver daemon: %w", err)
	}
	proc := newDaemonProc(cmd, group, stdinW)
	if err := waitSocket(ctx, socketPath); err != nil {
		_ = proc.teardown()
		return fmt.Errorf("computer: driver daemon socket: %w", err)
	}
	m.proc = proc
	return nil
}

// Restart is the grant-change hook (spec computer-use "Grant change restarts
// the daemon"): stop and start at what is by definition an idle boundary —
// the settings service calls it after a re-check flips a grant, before any
// ready state is reported.
func (m *Manager) Restart(ctx context.Context) error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start(ctx)
}

// Stop tears the daemon down: close the liveness pipe (the daemon's own
// exit path), SIGTERM the group, SIGKILL whatever outlives the grace, and
// reap. Stopping a stopped manager is a no-op. Any exit status is success
// once the kills are done.
func (m *Manager) Stop() error {
	m.mu.Lock()
	proc := m.proc
	m.proc = nil
	m.mu.Unlock()
	if proc == nil {
		return nil
	}
	return proc.teardown()
}

// waitSocket polls for the daemon's socket to appear, then tightens it to
// 0600 (the driver creates it with its own mode; the parent enforces the
// private-socket contract post-creation).
func waitSocket(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(socketWaitTimeout)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			// Best-effort: the private-socket mode is ours to enforce even
			// though the driver created the file.
			_ = os.Chmod(socketPath, 0o600)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("did not appear within %s", socketWaitTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(socketWaitPoll):
		}
	}
}

// daemonProc is one daemon generation: the command, its process group, the
// liveness pipe's write end, and the parent-liveness watchdog. teardown is
// the single graceful path; the watchdog the single crash path (mirrors
// browseruse's browserProc).
type daemonProc struct {
	group     groupHandle
	exited    chan struct{}
	stdinW    *os.File
	watchCtx  context.Context
	watchStop context.CancelFunc
}

// newDaemonProc wraps a started daemon: it starts the reaper goroutine that
// owns cmd.Wait and the crash-path watchdog.
func newDaemonProc(cmd *exec.Cmd, group groupHandle, stdinW *os.File) *daemonProc {
	p := &daemonProc{group: group, exited: make(chan struct{}), stdinW: stdinW}
	go func() {
		_ = cmd.Wait() // any exit status is fine; the kills are ours
		close(p.exited)
	}()
	p.watchCtx, p.watchStop = context.WithCancel(context.Background())
	go p.watchParent()
	return p
}

// hasExited reports whether the daemon's process has already exited (the
// reaper's Wait returned) — the liveness read behind Manager.Running and
// Start's dead-generation replacement.
func (p *daemonProc) hasExited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// watchParent is the crash path: when the app process dies, force-kill the
// daemon group so nothing outlives the app by more than one interval (the
// stdin liveness env is the daemon's own first layer; this is ours).
func (p *daemonProc) watchParent() {
	defer p.watchStop()
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.watchCtx.Done():
			return
		case <-p.exited:
			return
		case <-ticker.C:
		}
		if !parentDied(os.Getppid()) {
			continue
		}
		_ = p.stdinW.Close()
		_ = forceKillGroup(p.group, 0)
		return
	}
}

// teardown is the graceful path: close the liveness pipe so the daemon's own
// stdin watch exits it, terminate the group with grace, SIGKILL whatever
// outlives it, and reap. A daemon that already died is not an error. Safe to
// call more than once.
func (p *daemonProc) teardown() error {
	p.watchStop()
	_ = p.stdinW.Close() // EOF on the daemon's stdin: its own exit path
	_ = terminateGroup(p.group, 0, terminateGrace)
	// Bounded reap: terminateGroup already SIGKILLed, so Wait returns
	// promptly; the timer only guards a pathological reaper.
	timer := time.NewTimer(terminateGrace)
	defer timer.Stop()
	select {
	case <-p.exited:
	case <-timer.C:
	}
	return nil
}
