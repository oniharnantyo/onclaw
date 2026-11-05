package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The system server service semantics tests (rebrand-browser-use-system-mcp
// task 3.3, spec agent-mcp "System-provided server"): every mutation
// addressed at the system id is rejected, and the listing synthesizes the
// System row from live supervision state without any store row.

// TestSystemMCPServerMutationsRejected pins the reserved id: create, update,
// delete, and probe at browser-use fail with ErrSystemMCPServer and no store
// row ever appears.
func TestSystemMCPServerMutationsRejected(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	_, err := svc.CreateMCPServer(ctx, store.MCPServer{
		ID:        mcp.SystemServerID,
		Name:      "imposter",
		Transport: mcp.TransportStdio,
		Command:   "/bin/true",
		Enabled:   true,
	})
	require.ErrorIs(t, err, ErrSystemMCPServer)

	_, err = svc.UpdateMCPServer(ctx, store.MCPServer{
		ID:   mcp.SystemServerID,
		Name: "renamed",
	})
	require.ErrorIs(t, err, ErrSystemMCPServer)

	require.ErrorIs(t, svc.DeleteMCPServer(ctx, mcp.SystemServerID), ErrSystemMCPServer)

	_, err = svc.ProbeMCPServer(ctx, mcp.SystemServerID)
	require.ErrorIs(t, err, ErrSystemMCPServer)

	// No store row was created by any of the rejected calls.
	listed, err := fs.MCPServers().List(ctx)
	require.NoError(t, err)
	require.Empty(t, listed, "the system id must never materialize as a store row")
}

// TestMCPServersSynthesizesSystemRow pins the synthesized System row (spec
// scenario "System row appears without user setup"): with the supervision
// state seam wired, the listing carries the browser-use row — always enabled,
// its status and discovered-tool count from live supervision state — behind
// the System flag, while user rows keep System false and there is no store
// row behind it.
func TestMCPServersSynthesizesSystemRow(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	state := mcp.ServerState{Status: mcp.StatusConnected, StatusError: "", ToolCount: 11}
	svc := NewSettingsService(fs, WithSystemServerState(func() (mcp.ServerState, bool) {
		return state, true
	}))

	created, err := svc.CreateMCPServer(ctx, store.MCPServer{
		Name:      "github",
		Transport: mcp.TransportStreamableHTTP,
		URL:       "https://example.invalid/mcp",
		Enabled:   true,
	})
	require.NoError(t, err)

	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	user, system := listed[0], listed[1]
	require.Equal(t, created.ID, user.ID)
	require.False(t, user.System, "a stored row is never the system row")

	require.Equal(t, mcp.SystemServerID, system.ID)
	require.Equal(t, mcp.SystemMCPServerName, system.Name)
	require.Equal(t, mcp.TransportStdio, system.Transport)
	require.True(t, system.Enabled, "the system server is always enabled")
	require.True(t, system.System)
	require.Equal(t, mcp.StatusConnected, system.Status)
	require.Equal(t, 11, system.ToolCount)

	// No store row behind the synthesized entry.
	_, err = fs.MCPServers().ByID(ctx, mcp.SystemServerID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// A failed supervision state surfaces through the row, not the call.
	state = mcp.ServerState{Status: mcp.StatusError, StatusError: "respawning stopped", ToolCount: 0}
	listed, err = svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusError, listed[1].Status)
	require.Contains(t, listed[1].StatusError, "respawning stopped")
}

// TestMCPServersOmitsSystemRowWhenUnwired pins the un-wired posture: without
// the supervision seam — or when the runtime supervises no system server —
// the listing is exactly the stored rows, so existing deployments and tests
// see no change.
func TestMCPServersOmitsSystemRowWhenUnwired(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()

	svc := NewSettingsService(fs)
	created, err := svc.CreateMCPServer(ctx, store.MCPServer{
		Name:      "github",
		Transport: mcp.TransportStreamableHTTP,
		URL:       "https://example.invalid/mcp",
		Enabled:   true,
	})
	require.NoError(t, err)

	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, created.ID, listed[0].ID)

	// The seam wired, but the runtime provides no system server.
	svc = NewSettingsService(fs, WithSystemServerState(func() (mcp.ServerState, bool) {
		return mcp.ServerState{}, false
	}))
	listed, err = svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}
