package agents

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// workspaceFreeTools are the catalog tools that need no agent workspace; a
// runtime without a workspaces source still offers them. The scheduler is
// workspace-free too but needs a schedule service, so it is not in this
// list — every manager these subtests build without WithScheduler withholds
// it (withheld, not a failure).
var workspaceFreeTools = []string{ToolCurrentTime, ToolHTTPGet, ToolWebFetch, ToolWebSearch, ToolBrowser}

// workspaceFreeToolsPlusScheduler is the workspace-free offer of a runtime
// wired with a schedule service.
var workspaceFreeToolsPlusScheduler = append(append([]string{}, workspaceFreeTools...), ToolScheduler)

// workspaceTools are the catalog tools rooted at the agent's workspace.
var workspaceTools = []string{
	ToolLs, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob, ToolGrep,
	ToolExecute, ToolDeleteFile,
	ToolTaskCreate, ToolTaskGet, ToolTaskUpdate, ToolTaskList,
	ToolCreateDocument,
}

// fullCatalogNames is the expected catalog membership in display order.
var fullCatalogNames = []string{
	ToolCurrentTime, ToolHTTPGet,
	ToolLs, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob, ToolGrep,
	ToolExecute, ToolDeleteFile,
	ToolTaskCreate, ToolTaskGet, ToolTaskUpdate, ToolTaskList,
	ToolWebFetch, ToolWebSearch, ToolBrowser,
	ToolScheduler,
	ToolSaveDocument,
	ToolCreateDocument,
}

// TestCatalogTools pins the catalog membership in display order — the only
// source of what the gate can offer (spec agent-tools "Tool gate").
func TestCatalogTools(t *testing.T) {
	catalog := make([]string, 0, len(fullCatalogNames))
	for _, meta := range CatalogTools() {
		catalog = append(catalog, meta.Name)
	}
	require.Equal(t, fullCatalogNames, catalog)

	for _, name := range fullCatalogNames {
		_, ok := CatalogToolByName(name)
		require.True(t, ok, "catalog lookup must find %s", name)
	}
	_, ok := CatalogToolByName("mystery")
	require.False(t, ok)
}

// TestCatalogSaveDocumentMeta pins the save_document entry's display
// metadata: the Tools pane card and the allowlist chips render it with no
// bespoke wiring (spec add-knowledge-corpus "Save document tool in the
// catalog").
func TestCatalogSaveDocumentMeta(t *testing.T) {
	meta, ok := CatalogToolByName(ToolSaveDocument)
	require.True(t, ok)
	require.Equal(t, "Save document", meta.DisplayName)
	require.Equal(t, "file-plus", meta.IconKey)
	require.Contains(t, meta.Description, "knowledge document")
}

// TestCatalogSchedulerMeta pins the Scheduler entry's display metadata: the
// Tools pane card and the allowlist chips render it with no bespoke wiring
// (spec add-scheduler "Scheduler appears in the catalog surfaces").
func TestCatalogSchedulerMeta(t *testing.T) {
	meta, ok := CatalogToolByName(ToolScheduler)
	require.True(t, ok)
	require.Equal(t, "Scheduler", meta.DisplayName)
	require.Equal(t, "calendar-clock", meta.IconKey)
	require.NotEmpty(t, meta.Description)
}

// TestCatalogCreateDocumentMeta pins the create_document entry's display
// metadata: the Tools pane card and the allowlist chips render it with no
// bespoke wiring (spec add-create-document "Create document tool in the
// catalog"). The icon key must exist in the frontend Icon map.
func TestCatalogCreateDocumentMeta(t *testing.T) {
	meta, ok := CatalogToolByName(ToolCreateDocument)
	require.True(t, ok)
	require.Equal(t, "Create Document", meta.DisplayName)
	require.Equal(t, "file-text", meta.IconKey)
	require.NotEmpty(t, meta.Description)
}

// TestCatalogPlantaskMeta pins the plantask tools' display metadata: the
// Tools pane cards and allowlist chips render them with expected metadata
// (spec agent-tools "Task plan tools").
func TestCatalogPlantaskMeta(t *testing.T) {
	cases := []struct {
		name        string
		displayName string
		iconKey     string
		desc        string
	}{
		{ToolTaskCreate, "Create Task", "list-checks", "Create a new task in the workspace task plan."},
		{ToolTaskGet, "Get Task", "file", "Get details of a task by ID from the workspace task plan."},
		{ToolTaskUpdate, "Update Task", "edit", "Update a task's status, details, or dependencies in the workspace task plan."},
		{ToolTaskList, "List Tasks", "list", "List all tasks in the workspace task plan."},
	}
	for _, tc := range cases {
		meta, ok := CatalogToolByName(tc.name)
		require.True(t, ok, "lookup %s", tc.name)
		require.Equal(t, tc.displayName, meta.DisplayName)
		require.Equal(t, tc.iconKey, meta.IconKey)
		require.Equal(t, tc.desc, meta.Description)
	}
}

// TestResolveOfferedTools covers the three-way gate: a tool reaches the
// model iff it is in the catalog AND globally enabled AND permitted by the
// agent's allowlist (an empty allowlist means "all catalog tools").
func TestResolveOfferedTools(t *testing.T) {
	ctx := context.Background()

	t.Run("empty allowlist offers all workspace-free tools", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		// No workspaces source is wired: the workspace-rooted tools are
		// withheld, the workspace-free ones offered.
		require.Equal(t, workspaceFreeTools, names)
		require.Empty(t, surface.agentDir)
	})

	t.Run("empty allowlist offers the full catalog with a workspace", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()), WithScheduler(newTestSchedulerService(fs)),
			WithKnowledge(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, fullCatalogNames, names)
		require.NotEmpty(t, surface.agentDir, "the workspace is created on demand")
	})

	t.Run("narrowed allowlist intersects the catalog", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a", Tools: []string{ToolReadFile, "nonexistent"}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{ToolReadFile}, names)
		// The other five fs middleware tools are withheld via Disable config.
		require.Equal(t, []string{ToolLs, ToolWriteFile, ToolEditFile, ToolGlob, ToolGrep}, surface.fsDisabled)
	})

	t.Run("globally disabled tool is not offered even when allowed", func(t *testing.T) {
		fs := fake.New()
		require.NoError(t, fs.ToolStates().SetEnabled(ctx, ToolHTTPGet, false))
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{ToolCurrentTime, ToolWebFetch, ToolWebSearch, ToolBrowser}, names)
	})

	t.Run("globally disabled execute removes the shell", func(t *testing.T) {
		fs := fake.New()
		require.NoError(t, fs.ToolStates().SetEnabled(ctx, ToolExecute, false))
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		require.False(t, surface.shell)
	})

	t.Run("unknown allowlist names never invent tools", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Tools: []string{"shell"}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Empty(t, names)
	})

	t.Run("slugless agent has workspace tools withheld, not failed", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "legacy", Tools: []string{ToolReadFile, ToolHTTPGet}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{ToolHTTPGet}, names)
	})

	// The scheduler tool's fourth condition (spec add-scheduler): the runtime
	// must have been built with a schedule service — without one the entry is
	// withheld, not a failure. The subtests below pair it with one negative
	// per standard gate condition.
	t.Run("scheduler without a schedule service is withheld, not failed", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, workspaceFreeTools, names, "no scheduler: the entry is withheld")
	})

	t.Run("scheduler with a schedule service and an open allowlist is offered", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()), WithScheduler(newTestSchedulerService(fs)))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, workspaceFreeToolsPlusScheduler, names)
	})

	t.Run("globally disabled scheduler is not offered even when wired", func(t *testing.T) {
		fs := fake.New()
		require.NoError(t, fs.ToolStates().SetEnabled(ctx, ToolScheduler, false))
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()), WithScheduler(newTestSchedulerService(fs)))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, workspaceFreeTools, names)
	})

	t.Run("scheduler not in the agent's allowlist is not offered", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()), WithScheduler(newTestSchedulerService(fs)))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Tools: []string{ToolCurrentTime, ToolHTTPGet}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{ToolCurrentTime, ToolHTTPGet}, names)
	})

	// The create_document tool's gate conditions (spec add-create-document
	// "Create document tool in the catalog"): the standard three-way gate,
	// plus the workspace scope — a slugless agent has it withheld, not
	// failed.
	t.Run("create_document with a workspace and an open allowlist is offered", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Contains(t, names, ToolCreateDocument)
		require.True(t, surface.createDocument, "the preset appendix rides an offered tool")
	})

	t.Run("create_document not in the agent's allowlist is not offered", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a", Tools: []string{ToolReadFile, ToolHTTPGet}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.NotContains(t, names, ToolCreateDocument)
		require.False(t, surface.createDocument, "no tool offered, no preset appendix")
	})

	t.Run("globally disabled create_document is not offered even when allowed", func(t *testing.T) {
		fs := fake.New()
		require.NoError(t, fs.ToolStates().SetEnabled(ctx, ToolCreateDocument, false))
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.NotContains(t, names, ToolCreateDocument)
		require.False(t, surface.createDocument)
	})

	t.Run("slugless agent has create_document withheld, not failed", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "legacy", Tools: []string{ToolCreateDocument, ToolHTTPGet}})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{ToolHTTPGet}, names)
		require.False(t, surface.createDocument)
	})
}

// newTestSchedulerService wires a minimal scheduler service over the test
// store; the gate never ticks it, so no-op seams suffice.
func newTestSchedulerService(st store.Store) *scheduler.Service {
	return scheduler.New(st, noopRuns{}, stubSchedulerGateway{})
}

// TestTurnOffersGatedTools proves the gate runs inside a real turn: the
// fake model records the bound tool options per Stream call.
func TestTurnOffersGatedTools(t *testing.T) {
	ctx := context.Background()

	t.Run("opted-in tool is offered", func(t *testing.T) {
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "ok"}
		})
		agent := f.agent
		agent.Tools = []string{ToolCurrentTime}
		require.NoError(t, f.store.Agents().Update(ctx, &agent))

		sess := f.newSession(t)
		turn, err := f.mgr.Send(ctx, sess.ID, "hi")
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
		requireToolNames(t, f.model.toolsOf(0), ToolCurrentTime)
	})

	t.Run("globally disabled tool is denied", func(t *testing.T) {
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "ok"}
		})
		require.NoError(t, f.store.ToolStates().SetEnabled(ctx, ToolCurrentTime, false))
		agent := f.agent
		agent.Tools = []string{ToolCurrentTime, ToolHTTPGet}
		require.NoError(t, f.store.Agents().Update(ctx, &agent))

		sess := f.newSession(t)
		turn, err := f.mgr.Send(ctx, sess.ID, "hi")
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
		requireToolNames(t, f.model.toolsOf(0), ToolHTTPGet)
	})

	t.Run("not opted-in tool is denied", func(t *testing.T) {
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "ok"}
		})
		agent := f.agent
		agent.Tools = []string{ToolHTTPGet}
		require.NoError(t, f.store.Agents().Update(ctx, &agent))

		sess := f.newSession(t)
		turn, err := f.mgr.Send(ctx, sess.ID, "hi")
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
		requireToolNames(t, f.model.toolsOf(0), ToolHTTPGet)
	})
}

// TestCurrentTimeTool exercises the invoker directly.
func TestCurrentTimeTool(t *testing.T) {
	tool, err := newCurrentTimeTool()
	require.NoError(t, err)

	out, err := tool.InvokableRun(t.Context(), `{}`)
	require.NoError(t, err)
	require.Contains(t, out, `"utc"`)
	require.Contains(t, out, `"local"`)
	require.Contains(t, out, `"timezone"`)

	info, err := tool.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, ToolCurrentTime, info.Name)
}

// TestHTTPGetTool exercises the invoker against a local test server,
// including the size cap and the scheme restriction.
func TestHTTPGetTool(t *testing.T) {
	t.Run("fetches a body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "hello buddies")
		}))
		defer srv.Close()

		tool, err := newHTTPGetTool()
		require.NoError(t, err)
		out, err := tool.InvokableRun(t.Context(), fmt.Sprintf(`{"url": %q}`, srv.URL))
		require.NoError(t, err)
		require.Contains(t, out, `"status":200`)
		require.Contains(t, out, "hello buddies")
		require.NotContains(t, out, `"truncated":true`)
	})

	t.Run("rejects non-http schemes", func(t *testing.T) {
		tool, err := newHTTPGetTool()
		require.NoError(t, err)
		_, err = tool.InvokableRun(t.Context(), `{"url": "file:///etc/passwd"}`)
		require.ErrorContains(t, err, "unsupported URL scheme")
	})

	t.Run("truncates oversized bodies", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(make([]byte, httpGetMaxSize+100))
		}))
		defer srv.Close()

		tool, err := newHTTPGetTool()
		require.NoError(t, err)
		out, err := tool.InvokableRun(t.Context(), fmt.Sprintf(`{"url": %q}`, srv.URL))
		require.NoError(t, err)
		require.Contains(t, out, `"truncated":true`)
	})
}

// requireToolNames asserts a tool name list equals the expected names.
func requireToolNames(t *testing.T, names []string, want ...string) {
	t.Helper()
	require.Equal(t, want, names)
}

// infosToNames maps tool infos to their names.
func infosToNames(infos []*schema.ToolInfo) []string {
	if len(infos) == 0 {
		return nil
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}
