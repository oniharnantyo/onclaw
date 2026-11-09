package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The computer-use system server tests (add-computer-use tasks 2, design
// D2/D6, coordination contract C3): the spawn contract's exact argv, the
// registry's opt-in entry, and the supervisor's hold semantics — a
// config provider reporting ErrSystemServerHold surfaces StatusHeld, spawns
// nothing, moves no failure bookkeeping, and re-checks on later ticks.

// TestSystemComputerUseRefMatchesContract pins the coordination contract's
// exact argv for the computer-use child.
func TestSystemComputerUseRefMatchesContract(t *testing.T) {
	ref := systemComputerUseRef("/usr/local/bin/buddies", ComputerServerConfig{
		SocketPath:  "/data/computer/cua-driver.sock",
		ComputerDir: "/data/computer",
		Ready:       true,
	})
	require.Equal(t, ComputerUseServerID, ref.ServerID)
	require.Equal(t, ComputerUseServerID, ref.Name, "the naming pass must yield mcp__computer-use__<tool>")
	require.Equal(t, TransportStdio, ref.Conn.Transport)
	require.Equal(t, "/usr/local/bin/buddies", ref.Conn.Command)
	require.Equal(t, []string{
		"computer-mcp",
		"--socket", "/data/computer/cua-driver.sock",
		"--computer-dir", "/data/computer",
		"--ready", "true",
	}, ref.Conn.Args, "the child argv must match the spawn contract exactly")
}

// TestSystemComputerUseRegistryEntry pins the registry addition: computer-use
// is a reserved system id with OptIn=true, next to the unchanged browser-use.
func TestSystemComputerUseRegistryEntry(t *testing.T) {
	require.Equal(t, []string{SystemServerID, ComputerUseServerID}, SystemServerIDs())

	m := NewMCPManager(
		withConnector(func(context.Context, Ref) (*connection, error) {
			return nil, errors.New("not dialed in this test")
		}),
		withManualSupervision(),
		WithSystemComputerUse(func(context.Context) (ComputerServerConfig, error) {
			return ComputerServerConfig{SocketPath: "/tmp/s", ComputerDir: "/tmp/c", Ready: false}, nil
		}),
	)
	t.Cleanup(m.Close)
	require.Len(t, m.systems, 1)

	entries := m.SystemServers()
	require.Len(t, entries, 1)
	require.Equal(t, ComputerUseServerID, entries[0].Ref.ServerID)
	require.True(t, entries[0].OptIn, "computer-use is opt-in (design D2): an empty allowlist never widens to the desktop")

	// The spec hooks are bound: the provider's snapshot reaches the Ref
	// synthesis.
	sup := m.systems[0]
	cfg, err := sup.spec.Config(context.Background())
	require.NoError(t, err)
	require.Equal(t, ComputerServerConfig{SocketPath: "/tmp/s", ComputerDir: "/tmp/c", Ready: false},
		cfg.(ComputerServerConfig))
}

// TestSystemSupervisorHoldSemantics is the C3 proof: a provider returning
// the hold surfaces StatusHeld with the wrapped reason, spawns nothing,
// never increments the failure count (an arbitrary number of held ticks
// cannot reach the cap), and the next ready snapshot spawns immediately.
func TestSystemSupervisorHoldSemantics(t *testing.T) {
	spawner := &fakeSpawner{}
	clock := newFakeClock()
	phase := "held"
	reason := fmt.Errorf("%w: driver download pending", ErrSystemServerHold)

	m := NewMCPManager(
		withConnector(func(context.Context, Ref) (*connection, error) {
			return nil, errors.New("the fake spawner must own system spawns")
		}),
		withManualSupervision(),
		WithSystemComputerUse(func(context.Context) (ComputerServerConfig, error) {
			if phase == "held" {
				return ComputerServerConfig{}, reason
			}
			return ComputerServerConfig{SocketPath: "/tmp/s", ComputerDir: "/tmp/c", Ready: true}, nil
		}),
	)
	t.Cleanup(m.Close)
	sup := m.systems[0]
	sup.spawn = spawner.spawn
	sup.now = clock.Now

	// Tick while held: the held status surfaces with the reason, and nothing
	// spawns.
	for i := 0; i < 10; i++ {
		sup.tick()
	}
	require.Zero(t, spawner.attempts(), "a held server never attempts a spawn")
	state, ok := m.SystemServerState(ComputerUseServerID)
	require.True(t, ok)
	require.Equal(t, StatusHeld, state.Status)
	require.Contains(t, state.StatusError, "driver download pending")

	// The held entry offers no tools: the Tools-facing error is readable.
	_, err := m.Tools(context.Background(), Ref{ServerID: ComputerUseServerID, Name: ComputerUseServerID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "awaiting readiness")
	require.Contains(t, err.Error(), "driver download pending")

	// Holding never feeds the failure bookkeeping: ten held ticks do not
	// approach the cap, and the supervisor is not capped.
	require.Zero(t, sup.failCount)
	require.False(t, sup.capped)

	// The hold clears: the very next tick spawns, and the status connects.
	phase = "ready"
	sup.tick()
	require.Equal(t, 1, spawner.attempts(), "the first ready snapshot spawns immediately")
	serverState, ok := m.SystemServerState(ComputerUseServerID)
	require.True(t, ok)
	require.Equal(t, StatusConnected, serverState.Status)
	require.Empty(t, serverState.StatusError)
	require.Equal(t, "computer-mcp", spawner.lastRef().Conn.Args[0])

	// Held again while a child is live: the config check keeps the healthy
	// child (a hold is a config-read answer, never a teardown).
	phase = "held"
	sup.tick()
	require.Equal(t, 1, spawner.attempts())
	serverState, _ = m.SystemServerState(ComputerUseServerID)
	require.Equal(t, StatusConnected, serverState.Status, "a live child survives a held config check")
}

// TestSystemSupervisorHoldThenFailureCountsNormally: a hold never masks a
// real failure — after the hold clears, failed attempts count toward the cap
// exactly as before.
func TestSystemSupervisorHoldThenFailureCountsNormally(t *testing.T) {
	spawner := &fakeSpawner{}
	clock := newFakeClock()
	phase := "held"

	m := NewMCPManager(
		withConnector(func(context.Context, Ref) (*connection, error) {
			return nil, errors.New("the fake spawner must own system spawns")
		}),
		withManualSupervision(),
		WithSystemComputerUse(func(context.Context) (ComputerServerConfig, error) {
			if phase == "held" {
				return ComputerServerConfig{}, fmt.Errorf("%w: permissions pending", ErrSystemServerHold)
			}
			return ComputerServerConfig{SocketPath: "/tmp/s", ComputerDir: "/tmp/c", Ready: true}, nil
		}),
	)
	t.Cleanup(m.Close)
	sup := m.systems[0]
	sup.spawn = spawner.spawn
	sup.now = clock.Now

	sup.tick()
	require.Zero(t, spawner.attempts())
	boom := errors.New("spawn failed")
	spawner.queue = []error{boom, boom, boom, boom, boom}

	phase = "ready"
	for i := 0; i < 5; i++ {
		sup.tick()
		clock.Advance(SystemSpawnBackoffBase << i)
	}
	require.Equal(t, 5, spawner.attempts())
	serverState, ok := m.SystemServerState(ComputerUseServerID)
	require.True(t, ok)
	require.Equal(t, StatusError, serverState.Status)
	require.Contains(t, serverState.StatusError, "respawning stopped after 5 consecutive failures",
		"the cap counts real failures since the hold cleared, holds never counting")
}

// TestWithSystemComputerUseNilProviderIgnored: a nil provider registers
// nothing.
func TestWithSystemComputerUseNilProviderIgnored(t *testing.T) {
	m, _ := newTestManager(t, WithSystemComputerUse(nil))
	require.Empty(t, m.systems)
	require.Empty(t, m.SystemServers())
}

// TestHoldEntryStatusVocabulary pins the new vocabulary value.
func TestHoldEntryStatusVocabulary(t *testing.T) {
	require.Equal(t, "held", StatusHeld)
	require.NotEqual(t, StatusError, StatusHeld)
	require.NotEqual(t, StatusUnknown, StatusHeld)
}
