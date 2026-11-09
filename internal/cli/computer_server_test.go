//go:build server

package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/services"
)

// The server build's Computer Use composition test (add-computer-use design
// D9, spec computer-use "Desktop-only availability"): the whole lane is
// absent — no supervised spec, no supervision state, and the settings pane
// presents the capability as unavailable rather than as a toggle that cannot
// work.

func TestServerAssemble_OmitsComputerUse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// WithoutSystemBrowserUse keeps the test binary from being spawned as the
	// browser-use child (the server composition itself registers it — the
	// live server smoke exercises that against the real binary). The server
	// seam composes no computer-use spec, so nothing else is supervised.
	app, err := Assemble(ctx, &recordingEmitter{}, services.NoBrowser{}, nil, WithoutSystemBrowserUse())
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	require.Empty(t, app.mcpMgr.SystemServers())
	_, ok := app.mcpMgr.SystemServerState(mcp.ComputerUseServerID)
	require.False(t, ok)

	status, err := app.Services.Settings.ComputerStatus(ctx)
	require.NoError(t, err)
	require.False(t, status.Available)
	require.Equal(t, services.ComputerStatusUnavailable, status.Status)
}
