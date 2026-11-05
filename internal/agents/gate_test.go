package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// TestGate_PlantaskOfferedWithWorkspace covers that with a rooted workspace
// and unrestricted allowlist, all four plantask tools are offered by default.
func TestGate_PlantaskOfferedWithWorkspace(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "agent", Slug: "agent"}, "")
	require.NoError(t, err)

	names, err := surface.names(ctx)
	require.NoError(t, err)

	for _, toolName := range plantaskToolNames {
		require.Contains(t, names, toolName, "expected %s to be offered", toolName)
	}
	require.Empty(t, surface.plantaskDisabled, "no plantask tools should be disabled")
	require.NotEmpty(t, surface.agentDir)
}

// TestGate_PlantaskSluglessAgent covers that a slugless agent owns no workspace,
// so all workspace-scoped tools (including plantask tools) are withheld, not failed.
func TestGate_PlantaskSluglessAgent(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "slugless"}, "")
	require.NoError(t, err)

	names, err := surface.names(ctx)
	require.NoError(t, err)

	for _, toolName := range plantaskToolNames {
		require.NotContains(t, names, toolName, "plantask tool %s must not be offered without workspace", toolName)
	}
	require.Empty(t, surface.agentDir)
}

// TestGate_PlantaskAllowlistGating covers that an agent allowlist restricts which
// plantask tools are offered, and withheld plantask tools appear in plantaskDisabled.
func TestGate_PlantaskAllowlistGating(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	agent := store.Agent{
		Name:  "custom",
		Slug:  "custom",
		Tools: []string{ToolTaskCreate, ToolTaskList},
	}
	surface, err := m.resolveTurnTools(ctx, agent, "")
	require.NoError(t, err)

	names, err := surface.names(ctx)
	require.NoError(t, err)

	require.Contains(t, names, ToolTaskCreate)
	require.Contains(t, names, ToolTaskList)
	require.NotContains(t, names, ToolTaskGet)
	require.NotContains(t, names, ToolTaskUpdate)

	require.ElementsMatch(t, []string{ToolTaskGet, ToolTaskUpdate}, surface.plantaskDisabled)
}

// TestGate_PlantaskGloballyDisabled covers that a globally disabled plantask tool
// is withheld and placed in plantaskDisabled even if the agent allowlist is unrestricted.
func TestGate_PlantaskGloballyDisabled(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	require.NoError(t, fs.ToolStates().SetEnabled(ctx, ToolTaskCreate, false))

	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "agent", Slug: "agent"}, "")
	require.NoError(t, err)

	names, err := surface.names(ctx)
	require.NoError(t, err)

	require.NotContains(t, names, ToolTaskCreate)
	require.Contains(t, names, ToolTaskGet)
	require.Contains(t, names, ToolTaskUpdate)
	require.Contains(t, names, ToolTaskList)

	require.Contains(t, surface.plantaskDisabled, ToolTaskCreate)
}

// TestGate_PlantaskAllGloballyDisabled covers disabling all plantask tools in the Tools pane.
func TestGate_PlantaskAllGloballyDisabled(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	for _, toolName := range plantaskToolNames {
		require.NoError(t, fs.ToolStates().SetEnabled(ctx, toolName, false))
	}

	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "agent", Slug: "agent"}, "")
	require.NoError(t, err)

	names, err := surface.names(ctx)
	require.NoError(t, err)

	for _, toolName := range plantaskToolNames {
		require.NotContains(t, names, toolName)
	}
	require.ElementsMatch(t, plantaskToolNames, surface.plantaskDisabled)
}

// TestGate_NoBrowserCatalogEntry pins the catalog removal
// (rebrand-browser-use-system-mcp task 4.1, spec agent-tools delta "Browser
// tool in the catalog" REMOVED): no `browser` catalog entry exists, the gate
// never offers a `browser` tool, and even an allowlist explicitly naming
// `browser` — stale migrated data — invents nothing.
func TestGate_NoBrowserCatalogEntry(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
		WithWorkspaces(workspaces.NewManager(t.TempDir())),
		WithBrowserDir(t.TempDir()))

	for _, meta := range CatalogTools() {
		require.NotEqual(t, "browser", meta.Name, "the browser tool must not be in the catalog")
	}
	_, ok := CatalogToolByName("browser")
	require.False(t, ok, "the browser tool must not be resolvable from the catalog")

	// An unrestricted agent's turn carries no browser tool…
	agent := store.Agent{Name: "agent", Slug: "agent"}
	surface, err := m.resolveTurnTools(ctx, agent, "")
	require.NoError(t, err)
	names, err := surface.names(ctx)
	require.NoError(t, err)
	require.NotContains(t, names, "browser")

	// …and neither does one whose stale allowlist still names it.
	agent.Tools = []string{"browser"}
	surface, err = m.resolveTurnTools(ctx, agent, "")
	require.NoError(t, err)
	names, err = surface.names(ctx)
	require.NoError(t, err)
	require.NotContains(t, names, "browser")
}
