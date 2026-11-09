package computeruse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The driver daemon lifecycle tests (add-computer-use tasks 2.4, design D1,
// spec computer-use "Driver daemon lifecycle"): a compiled stub driver
// binary stands in for the vendored daemon — the socket appears, the child's
// environment is exactly the five-row telemetry-off contract, a stop (and
// the stdin-liveness path the parent death relies on) leaves no process
// behind, and Start is idempotent.

var (
	compileStubOnce sync.Once
	stubBin         string
	stubCompileErr  error
)

// compileDriverStub builds the testdata driver stub once for the whole test
// binary (the mcp package's mockmcpserver pattern).
func compileDriverStub(t *testing.T) string {
	t.Helper()
	compileStubOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			stubCompileErr = errors.New("cannot locate test source dir")
			return
		}
		pkgDir := filepath.Dir(thisFile)
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			goBin = "go"
		}
		dir, err := os.MkdirTemp("", "buddies-cua-stub")
		if err != nil {
			stubCompileErr = err
			return
		}
		stubBin = filepath.Join(dir, "driverstub")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", stubBin, "./testdata/driverstub")
		cmd.Dir = pkgDir
		if out, err := cmd.CombinedOutput(); err != nil {
			stubCompileErr = fmt.Errorf("compile driverstub: %v\n%s", err, out)
		}
	})
	if stubCompileErr != nil {
		t.Fatalf("driver stub unavailable: %v", stubCompileErr)
	}
	return stubBin
}

// await polls until the predicate holds or the deadline passes.
func await(t *testing.T, what string, deadline time.Duration, done func() bool) {
	t.Helper()
	timer := time.After(deadline)
	for {
		if done() {
			return
		}
		select {
		case <-timer:
			t.Fatalf("%s: not satisfied within %s", what, deadline)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// readStubEnv parses the stub's env record into a map.
func readStubEnv(t *testing.T, socketPath string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(socketPath + ".env")
	require.NoError(t, err)
	env := map[string]string{}
	for _, row := range strings.Split(string(data), "\n") {
		if i := strings.Index(row, "="); i > 0 {
			env[row[:i]] = row[i+1:]
		}
	}
	return env
}

// TestDriverManagerSpawnsStubWithEnvContract: Start spawns the stub as a
// direct child, the socket appears, and the child's environment is exactly
// the five-row contract — nothing more.
func TestDriverManagerSpawnsStubWithEnvContract(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	mgr := NewManager(stub, computerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	require.True(t, mgr.Running())
	t.Cleanup(func() { _ = mgr.Stop() })

	socketPath := mgr.SocketPath()
	require.Equal(t, filepath.Join(computerDir, SocketFileName), socketPath)
	await(t, "socket appears", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})

	// The private-socket contract: the parent tightens the mode to 0600.
	info, err := os.Stat(socketPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600).Perm(), info.Mode().Perm(),
		"the socket is private to the user, got %s", info.Mode())

	// The env contract is exact: five rows, telemetry and update checks off.
	env := readStubEnv(t, socketPath)
	require.Len(t, env, 5, "the driver env contract is exactly these rows, got %v", env)
	require.Equal(t, "1", env["DO_NOT_TRACK"])
	require.Equal(t, "0", env["CUA_DRIVER_RS_TELEMETRY_ENABLED"])
	require.Equal(t, "0", env["CUA_DRIVER_RS_UPDATE_CHECK"])
	require.Equal(t, "1", env["CUA_DRIVER_EMBEDDED"])
	require.Equal(t, "1", env["CUA_DRIVER_PARENT_LIVENESS_STDIN"])
}

// TestDriverManagerStartIdempotent: a running daemon is left untouched —
// the same generation answers both Starts (one env record, unchanged).
func TestDriverManagerStartIdempotent(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	mgr := NewManager(stub, computerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	t.Cleanup(func() { _ = mgr.Stop() })
	socketPath := mgr.SocketPath()
	await(t, "socket appears", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})
	first, err := os.ReadFile(socketPath + ".env")
	require.NoError(t, err)

	require.NoError(t, mgr.Start(ctx), "Start on a running manager is a no-op")
	require.True(t, mgr.Running())
	second, err := os.ReadFile(socketPath + ".env")
	require.NoError(t, err)
	require.Equal(t, string(first), string(second), "the same daemon generation still runs")
}

// TestDriverManagerStopLeavesNoProcess: Stop closes the liveness pipe (the
// daemon's own exit path), terminates the group, and the stub exits — the
// env record disappears, and a stopped manager reports not running.
func TestDriverManagerStopLeavesNoProcess(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	mgr := NewManager(stub, computerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	socketPath := mgr.SocketPath()
	await(t, "socket appears", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})

	require.NoError(t, mgr.Stop())
	require.False(t, mgr.Running())
	require.NoError(t, mgr.Stop(), "Stop is idempotent")
	await(t, "stub exits on liveness EOF", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath + ".env")
		return os.IsNotExist(err)
	})
}

// TestDriverManagerRestartHook: the grant-change hook stops the old
// generation and spawns a fresh one — the spec's "grant change restarts the
// daemon before the ready state is reported".
func TestDriverManagerRestartHook(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	mgr := NewManager(stub, computerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	socketPath := mgr.SocketPath()
	await(t, "socket appears", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})
	require.NoError(t, mgr.Stop())
	await(t, "first generation exits", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath + ".env")
		return os.IsNotExist(err)
	})

	require.NoError(t, mgr.Restart(ctx), "the grant-change hook restarts the daemon")
	require.True(t, mgr.Running())
	t.Cleanup(func() { _ = mgr.Stop() })
	await(t, "second generation socket appears", 10*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})
	env := readStubEnv(t, socketPath)
	require.Equal(t, "1", env["CUA_DRIVER_EMBEDDED"], "the fresh generation carries the env contract")
}

// TestDriverManagerStartRequiresBinary: an unresolved binary is a startup
// failure with a readable reason, not a spawn.
func TestDriverManagerStartRequiresBinary(t *testing.T) {
	mgr := NewManager("", t.TempDir())
	err := mgr.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not resolved")

	mgr = NewManager(filepath.Join(t.TempDir(), "missing-driver"), t.TempDir())
	err = mgr.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "driver binary")
	require.False(t, mgr.Running())
}

// TestDriverManagerStaleSocketCleared: a dead daemon's leftover socket file
// does not wedge the next spawn.
func TestDriverManagerStaleSocketCleared(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	stale := filepath.Join(computerDir, SocketFileName)
	require.NoError(t, os.MkdirAll(computerDir, 0o700))
	require.NoError(t, os.WriteFile(stale, []byte("stale"), 0o600))

	mgr := NewManager(stub, computerDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	t.Cleanup(func() { _ = mgr.Stop() })
	require.True(t, mgr.Running())
}

// TestDriverManagerReplacesDeadGeneration covers the reconciliation heal
// (add-computer-use spec "Driver daemon lifecycle"): a daemon that exits on
// its own reports down — Running reads the process, not the memory of a
// spawn — and the next Start replaces the dead generation with a fresh one
// (clearing the socket it left behind) instead of no-oping.
func TestDriverManagerReplacesDeadGeneration(t *testing.T) {
	stub := compileDriverStub(t)
	computerDir := t.TempDir()
	mgr := NewManager(stub, computerDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, mgr.Start(ctx))
	t.Cleanup(func() { _ = mgr.Stop() })
	require.True(t, mgr.Running())

	// The daemon dies on its own — stdin EOF is the daemon's own first
	// liveness layer (CUA_DRIVER_PARENT_LIVENESS_STDIN=1), the path any
	// crash reduces to. The stub removes its env record on exit, so the
	// file's disappearance is the exit marker.
	socketPath := filepath.Join(computerDir, SocketFileName)
	envPath := socketPath + ".env"
	_, err := os.Stat(envPath)
	require.NoError(t, err, "the running stub records its env beside the socket")
	require.NoError(t, mgr.proc.stdinW.Close())
	await(t, "the dead generation's exit", 10*time.Second, func() bool {
		_, err := os.Stat(envPath)
		return os.IsNotExist(err)
	})
	await(t, "the manager reports the dead generation down", 10*time.Second, func() bool {
		return !mgr.Running()
	})

	// The next Start spawns a fresh generation over the stale socket.
	require.NoError(t, mgr.Start(ctx))
	await(t, "the fresh generation's env record", 10*time.Second, func() bool {
		_, err := os.Stat(envPath)
		return err == nil
	})
	require.True(t, mgr.Running())
}
