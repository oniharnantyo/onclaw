package agents

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The binding isolation at the parent layer (add-per-agent-browser-profiles
// task 4.1, design D2, spec "Agents do not share a live browser" / "Agent
// actions use the agent's own profile"): two agents resolved through the same
// manager carry two different workspace profile directories — each its own
// <workspace>/<slug>/browser-profile — so no two agents ever share one
// profile, and the display name of each binding is that agent's own slug.

// newTwoAgentWrapManager wires a manager whose workspace manager roots a
// temp dir with a workspace for every slug, returning the manager plus the
// workspace root. Mirrors newWrapManager for more than one agent.
func newTwoAgentWrapManager(t *testing.T, slugs ...string) (*manager, string) {
	t.Helper()
	fs := fake.New()
	ws := workspaces.NewManager(t.TempDir())
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(ws))
	for _, slug := range slugs {
		_, err := ws.Create(slug)
		require.NoError(t, err)
	}
	return m, ws.Root()
}

// TestTwoAgentsBindTheirOwnProfileDirs is the agents-layer isolation pin
// (task 4.1): one parent resolves two agents' browser tools, and each agent's
// wrapped browser-use tool stamps a different buddies.dev/profile-dir — its
// own workspace's browser-profile directory — with the agent's slug as the
// display name. The binding values are per-agent by construction: distinct
// slugs resolve distinct workspace directories.
func TestTwoAgentsBindTheirOwnProfileDirs(t *testing.T) {
	const (
		scoutSession = "e7238190a1b2c3d4e5f60718293a4b5c"
		houndSession = "f8349201a2b3c4d5e6f708192a3b4c6d"
	)
	m, wsRoot := newTwoAgentWrapManager(t, "scout", "hound")
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	scoutTools := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID},
		[]tool.BaseTool{&stubRawTool{name: "navigate"}}, store.Agent{Slug: "scout"},
		attentionScope{sessionID: scoutSession})
	houndTools := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID},
		[]tool.BaseTool{&stubRawTool{name: "navigate"}}, store.Agent{Slug: "hound"},
		attentionScope{sessionID: houndSession})
	require.Len(t, scoutTools, 1)
	require.Len(t, houndTools, 1)

	// Each agent invokes its own wrapped tool; the shared invoker records the
	// metas in call order.
	_, err := scoutTools[0].(tool.InvokableTool).InvokableRun(context.Background(), `{"url":"https://site.test/"}`)
	require.NoError(t, err)
	_, err = houndTools[0].(tool.InvokableTool).InvokableRun(context.Background(), `{"url":"https://site.test/"}`)
	require.NoError(t, err)

	_, _, metas := inv.snapshot()
	require.Len(t, metas, 2, "one meta-bearing invocation per agent")
	scoutDir := filepath.Join(wsRoot, "scout", workspaces.DirBrowserProfile)
	houndDir := filepath.Join(wsRoot, "hound", workspaces.DirBrowserProfile)
	require.Equal(t, scoutDir, metas[0][MetaProfileDir],
		"scout's call carries scout's workspace profile directory")
	require.Equal(t, houndDir, metas[1][MetaProfileDir],
		"hound's call carries hound's workspace profile directory")
	require.NotEqual(t, metas[0][MetaProfileDir], metas[1][MetaProfileDir],
		"no two agents share one profile")
	require.Equal(t, "scout", metas[0][MetaProfileName],
		"the display name is the calling agent's own slug")
	require.Equal(t, "hound", metas[1][MetaProfileName],
		"the display name is the calling agent's own slug")

	// Each binding's directory exists on demand inside its own workspace:
	// the resolution created them, ready for the child's first action.
	require.DirExists(t, scoutDir)
	require.DirExists(t, houndDir)
}
