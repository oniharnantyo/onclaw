//go:build !server

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/services"
)

// The desktop composition's Computer Use lane tests (add-computer-use tasks
// 2.6 + 3.1's wiring): the supervised spec registers with the warm-up gate
// holding the disabled lane (zero tools, status held), the settings hooks
// wire, and WithoutSystemComputerUse composes nothing. The server build's
// mirror lives in computer_server_test.go.

func TestAssemble_ComputerUseSpecRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// WithoutSystemBrowserUse keeps the test binary from being spawned as the
	// browser-use child (app_test.go's posture; the both-specs live spawn is
	// the server-mode smoke's job). Computer-use stays registered: its
	// provider holds while the lane is disabled, so nothing spawns here
	// either.
	app, err := Assemble(ctx, &recordingEmitter{}, services.NoBrowser{}, nil, WithoutSystemBrowserUse())
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	entries := app.mcpMgr.SystemServers()
	require.Len(t, entries, 1)
	require.Equal(t, mcp.ComputerUseServerID, entries[0].Ref.ServerID)
	// Opt-in (add-computer-use design D2): desktop control is offered only to
	// agents whose allowlist names it.
	require.True(t, entries[0].OptIn)

	// The warm-up gate holds the disabled lane: no spawn attempt, the held
	// status, and the provider's readable hold (coordination contract C3).
	require.Eventually(t, func() bool {
		state, ok := app.mcpMgr.SystemServerState(mcp.ComputerUseServerID)
		return ok && state.Status == mcp.StatusHeld
	}, 3*time.Second, 20*time.Millisecond)
	_, err = app.computer.spawnConfig(ctx)
	require.ErrorIs(t, err, mcp.ErrSystemServerHold)
}

func TestAssemble_ComputerUseHooksWired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	app, err := Assemble(ctx, &recordingEmitter{}, services.NoBrowser{}, nil, WithoutSystemBrowserUse())
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	// The pane's aggregate reads through the wired hooks: available on
	// desktop builds, the stored lane off (opt-in end to end, design D2),
	// the driver not yet acquired.
	status, err := app.Services.Settings.ComputerStatus(ctx)
	require.NoError(t, err)
	require.True(t, status.Available)
	require.False(t, status.Enabled)
	require.Equal(t, services.ComputerStatusOff, status.Status)
	require.Equal(t, services.ComputerDriverPending, status.Driver.State)
}

func TestAssemble_WithoutSystemComputerUse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	app, err := Assemble(context.Background(), &recordingEmitter{}, services.NoBrowser{},
		nil, WithoutSystemBrowserUse(), WithoutSystemComputerUse())
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	// No computer-use spec, and the manager supervises nothing under it.
	require.Empty(t, app.mcpMgr.SystemServers())
	_, ok := app.mcpMgr.SystemServerState(mcp.ComputerUseServerID)
	require.False(t, ok)
}

// TestComputerLaneReconcileHoldsClosedGate covers the reconciliation's gate
// side (add-computer-use spec "Driver daemon lifecycle"): a hold — the
// disabled lane here, the same spawnConfig the supervisor reads (contract
// C3) — spawns no daemon, and the reconcile tick is a silent no-op. The
// ready-path heal itself is the live smoke's job (real daemon, real grants).
func TestComputerLaneReconcileHoldsClosedGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	app, err := Assemble(ctx, &recordingEmitter{}, services.NoBrowser{}, nil, WithoutSystemBrowserUse())
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	// The disabled lane's warm-up loop is already ticking (Assemble started
	// it); a direct reconcile is what one tick runs.
	app.computer.reconcile(ctx)

	require.Nil(t, app.computer.daemon, "a held gate must not spawn the daemon")
	require.False(t, app.computer.daemonRunning())
	// The held status is written by the supervisor's own first tick, which
	// races this test under load — wait for it deterministically instead of
	// asserting the goroutine has already landed.
	require.Eventually(t, func() bool {
		state, ok := app.mcpMgr.SystemServerState(mcp.ComputerUseServerID)
		return ok && state.Status == mcp.StatusHeld
	}, 2*time.Second, 10*time.Millisecond, "the disabled lane's supervisor surfaces the hold")
}
