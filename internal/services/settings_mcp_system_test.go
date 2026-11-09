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
// addressed at the system id is rejected, the listing synthesizes the System
// rows from live supervision state without any store row, and the probe of a
// supervised system server reports that same live state.

// TestSystemMCPServerMutationsRejected pins the reserved ids (add-computer-use
// design D2: the guard enumerates the registry, so every registered system
// server id is rejected): create, update, and delete at each registered id
// fail with ErrSystemMCPServer, and so does probe when the runtime supervises
// no system server (the un-wired posture of this fixture — nothing to
// report). The supervised probe path is TestProbeMCPServerSystemRow below.
// No store row ever appears.
func TestSystemMCPServerMutationsRejected(t *testing.T) {
	ctx := context.Background()
	for _, systemID := range mcp.SystemServerIDs() {
		t.Run(systemID, func(t *testing.T) {
			fs := fake.New()
			svc := NewSettingsService(fs)

			_, err := svc.CreateMCPServer(ctx, store.MCPServer{
				ID:        systemID,
				Name:      "imposter",
				Transport: mcp.TransportStdio,
				Command:   "/bin/true",
				Enabled:   true,
			})
			require.ErrorIs(t, err, ErrSystemMCPServer)

			_, err = svc.UpdateMCPServer(ctx, store.MCPServer{
				ID:   systemID,
				Name: "renamed",
			})
			require.ErrorIs(t, err, ErrSystemMCPServer)

			require.ErrorIs(t, svc.DeleteMCPServer(ctx, systemID), ErrSystemMCPServer)

			_, err = svc.ProbeMCPServer(ctx, systemID)
			require.ErrorIs(t, err, ErrSystemMCPServer)

			// The seam wired but the runtime supervises no system server with
			// the id: still nothing to report, still rejected.
			svc = NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
				return mcp.ServerState{}, false
			}))
			_, err = svc.ProbeMCPServer(ctx, systemID)
			require.ErrorIs(t, err, ErrSystemMCPServer)

			// No store row was created by any of the rejected calls.
			listed, err := fs.MCPServers().List(ctx)
			require.NoError(t, err)
			require.Empty(t, listed, "the system id must never materialize as a store row")
		})
	}
}

// TestProbeMCPServerSystemRow pins the on-demand probe of a supervised system
// server (spec agent-mcp "System-provided server": the system server behaves
// exactly like a user server, probed on demand): the supervision loop is the
// row's prober — it spawns the child and feeds its status — so probing a
// registered id the runtime supervises returns the live-state System row
// without touching any store row, whether supervision reports connected or
// failed.
func TestProbeMCPServerSystemRow(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()

	for _, systemID := range mcp.SystemServerIDs() {
		t.Run(systemID, func(t *testing.T) {
			state := mcp.ServerState{Status: mcp.StatusConnected, ToolCount: 11}
			svc := NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
				if id != systemID {
					return mcp.ServerState{}, false
				}
				return state, true
			}))

			probed, err := svc.ProbeMCPServer(ctx, systemID)
			require.NoError(t, err)
			require.True(t, probed.System, "the probed system row keeps its System marker")
			require.Equal(t, systemID, probed.ID)
			name, ok := mcp.SystemServerDisplayName(systemID)
			require.True(t, ok)
			require.Equal(t, name, probed.Name)
			require.Equal(t, mcp.TransportStdio, probed.Transport)
			require.True(t, probed.Enabled, "the system server is always enabled")
			require.Equal(t, mcp.StatusConnected, probed.Status)
			require.Equal(t, 11, probed.ToolCount)

			// A failed supervision state surfaces through the row, not the call.
			state = mcp.ServerState{Status: mcp.StatusError, StatusError: "spawn failed", ToolCount: 0}
			probed, err = svc.ProbeMCPServer(ctx, systemID)
			require.NoError(t, err)
			require.True(t, probed.System)
			require.Equal(t, mcp.StatusError, probed.Status)
			require.Equal(t, "spawn failed", probed.StatusError)

			// Probing a system id never materializes a store row.
			listed, err := fs.MCPServers().List(ctx)
			require.NoError(t, err)
			require.Empty(t, listed)
		})
	}
}

// TestMCPServersSynthesizesSystemRow pins the synthesized System rows (spec
// scenario "System row appears without user setup"; add-computer-use design
// D2 generalizes the synthesis to the registry): with the supervision state
// seam wired, the listing carries one row per registered system server the
// runtime supervises — always enabled, status and discovered-tool count from
// live supervision state — behind the System flag, while user rows keep
// System false and there is no store row behind any of them.
func TestMCPServersSynthesizesSystemRow(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	state := mcp.ServerState{Status: mcp.StatusConnected, StatusError: "", ToolCount: 11}
	svc := NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
		if id != mcp.SystemServerID {
			return mcp.ServerState{}, false
		}
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

// TestMCPServersSynthesizesEverySupervisedSystemRow pins the registry
// generalization (add-computer-use design D2): a seam that answers for every
// registered id yields one System row per registered system server, in
// registry order, each named by its spec's display name.
func TestMCPServersSynthesizesEverySupervisedSystemRow(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
		return mcp.ServerState{Status: mcp.StatusConnected, ToolCount: 3}, true
	}))

	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	ids := mcp.SystemServerIDs()
	require.Len(t, listed, len(ids))
	for i, id := range ids {
		require.True(t, listed[i].System)
		require.Equal(t, id, listed[i].ID)
		name, ok := mcp.SystemServerDisplayName(id)
		require.True(t, ok)
		require.Equal(t, name, listed[i].Name)
	}
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
	svc = NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
		return mcp.ServerState{}, false
	}))
	listed, err = svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

// TestProbeMCPServerReArmsCappedSupervision pins the cap's manual recovery
// path: when supervision has stopped respawning (the crash-loop cap), the
// on-demand probe re-arms it — the supervisor's explicit restart — and
// reports the re-armed state (unknown until the next tick's spawn lands),
// while a live supervision is left strictly alone.
func TestProbeMCPServerReArmsCappedSupervision(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	state := mcp.ServerState{
		Status:      mcp.StatusError,
		StatusError: "browser-use spawn failed: handshake timeout; respawning stopped after 5 consecutive failures",
		Capped:      true,
	}
	var restarted []string
	svc := NewSettingsService(fs,
		WithSystemServerState(func(id string) (mcp.ServerState, bool) {
			if id != mcp.SystemServerID {
				return mcp.ServerState{}, false
			}
			return state, true
		}),
		WithSystemServerRestart(func(id string) {
			restarted = append(restarted, id)
			state = mcp.ServerState{Status: mcp.StatusUnknown}
		}))

	probed, err := svc.ProbeMCPServer(ctx, mcp.SystemServerID)
	require.NoError(t, err)
	require.Equal(t, []string{mcp.SystemServerID}, restarted, "the probe re-arms a capped supervision")
	require.Equal(t, mcp.StatusUnknown, probed.Status, "the re-armed supervisor owns no state yet")
	require.True(t, probed.System)

	// A live supervision is never restarted by the probe: restarting a
	// healthy child would close the user's browser with it.
	state = mcp.ServerState{Status: mcp.StatusConnected, ToolCount: 11}
	probed, err = svc.ProbeMCPServer(ctx, mcp.SystemServerID)
	require.NoError(t, err)
	require.Len(t, restarted, 1)
	require.Equal(t, mcp.StatusConnected, probed.Status)
	require.Equal(t, 11, probed.ToolCount)

	// Capped without a restart seam: the capped state reports as-is.
	svc = NewSettingsService(fs, WithSystemServerState(func(id string) (mcp.ServerState, bool) {
		if id != mcp.SystemServerID {
			return mcp.ServerState{}, false
		}
		return mcp.ServerState{Status: mcp.StatusError, StatusError: "respawning stopped", Capped: true}, true
	}))
	probed, err = svc.ProbeMCPServer(ctx, mcp.SystemServerID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusError, probed.Status)
	require.Contains(t, probed.StatusError, "respawning stopped")
}
