package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The retired browser tool's allowlist migration tests
// (rebrand-browser-use-system-mcp task 4.2): agents seeded the way the old
// build left them — a `browser` entry in tools — migrate to the `browser-use`
// server key on open; already-migrated and unrelated agents are untouched,
// and a second open writes nothing.

// seedAllowlistAgents creates agents carrying the given tool and MCP server
// allowlists through the store API, then closes the store — the way a
// pre-change database's rows sit before the next open's migration runs.
func seedAllowlistAgents(t *testing.T, path string, seeds map[string][]string, mcpSeeds map[string][]string) {
	t.Helper()
	ctx := context.Background()

	st, err := Open(ctx, path)
	require.NoError(t, err, "seeding the fixture store must succeed")
	for id, tools := range seeds {
		agent := &store.Agent{ID: id, Name: id, Tools: tools, MCPServers: mcpSeeds[id]}
		require.NoError(t, st.Agents().Create(ctx, agent), "seed agent %s", id)
	}
	require.NoError(t, st.Close())
}

// allowlistsByID reads every agent's tools and mcp_servers allowlists by id.
func allowlistsByID(t *testing.T, st *Store) map[string][2][]string {
	t.Helper()
	agents, err := st.Agents().List(context.Background())
	require.NoError(t, err)
	byID := make(map[string][2][]string, len(agents))
	for _, a := range agents {
		byID[a.ID] = [2][]string{a.Tools, a.MCPServers}
	}
	return byID
}

// TestOpen_MigratesBrowserAllowlist covers the map: a pinned `browser` entry
// moves to `browser-use` in mcp_servers — alone and beside other entries,
// with and without an existing server list — while agents without the entry
// are untouched. An agent whose tools list was exactly ["browser"] ends with
// browser-use in MCPServers and no stale browser entry in Tools.
func TestOpen_MigratesBrowserAllowlist(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/allowlist.db"

	seedAllowlistAgents(t, path,
		map[string][]string{
			"pinned-only":  {"browser"},
			"pinned-mixed": {"browser", "current_time"},
			"unrelated":    {"current_time"},
			"empty":        {},
		},
		map[string][]string{
			"pinned-only":  {},
			"pinned-mixed": {"srv-1"},
			"unrelated":    {},
			"empty":        {"srv-1"},
		})

	st, err := Open(ctx, path)
	require.NoError(t, err, "the migrated database must open cleanly")
	t.Cleanup(func() { _ = st.Close() })
	sqliteStore := st.(*Store)

	got := allowlistsByID(t, sqliteStore)
	require.Len(t, got, 4)
	require.Equal(t, [2][]string{{}, {"browser-use"}}, got["pinned-only"],
		"a tools list of exactly [browser] ends as browser-use in MCPServers, no stale browser entry")
	require.Equal(t, [2][]string{{"current_time"}, {"srv-1", "browser-use"}}, got["pinned-mixed"],
		"the remaining tool entries keep their order and browser-use joins the existing server list")
	require.Equal(t, [2][]string{{"current_time"}, {}}, got["unrelated"],
		"an agent without a browser entry is untouched")
	require.Equal(t, [2][]string{{}, {"srv-1"}}, got["empty"],
		"an empty tools list is untouched: nothing to migrate")
}

// TestOpen_MigratesBrowserAllowlistIdempotent proves the migration is a
// no-op once applied: a second open leaves every allowlist — and every
// updated_at stamp, which a rewrite would refresh — exactly as the first
// migration left them.
func TestOpen_MigratesBrowserAllowlistIdempotent(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/idempotent.db"

	seedAllowlistAgents(t, path,
		map[string][]string{"pinned": {"browser"}},
		map[string][]string{"pinned": {}})

	st, err := Open(ctx, path)
	require.NoError(t, err)
	agents, err := st.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Equal(t, []string{"browser-use"}, agents[0].MCPServers)
	require.NoError(t, st.Close())

	st2, err := Open(ctx, path)
	require.NoError(t, err, "re-opening the migrated database must succeed")
	t.Cleanup(func() { _ = st2.Close() })

	agents2, err := st2.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, agents2, 1)
	require.Equal(t, agents[0].Tools, agents2[0].Tools)
	require.Equal(t, agents[0].MCPServers, agents2[0].MCPServers)
	require.Equal(t, agents[0].UpdatedAt, agents2[0].UpdatedAt,
		"the second migration run must not rewrite the row")
}
