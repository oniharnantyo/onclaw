package agents

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The session-dir wrap (add-browser-login-handover follow-up): every
// model-invoked browser-use call carries the calling session's workspace
// screenshots directory through the call's _meta, so the child writes
// captures inside the workspace jail. The metadata — never the arguments,
// which the transcript records — is the only channel, and the model can
// neither see nor influence it.

// fakeMetaInvoker records Invoke calls and answers with a canned result.
type fakeMetaInvoker struct {
	mu    sync.Mutex
	refs  []mcp.Ref
	names []string
	args  []string
	metas []map[string]any
	out   string
	err   error
}

func (f *fakeMetaInvoker) Invoke(_ context.Context, ref mcp.Ref, name, argsJSON string, meta map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs = append(f.refs, ref)
	f.names = append(f.names, name)
	f.args = append(f.args, argsJSON)
	f.metas = append(f.metas, meta)
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

func (f *fakeMetaInvoker) snapshot() (names, args []string, metas []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.names...), append([]string(nil), f.args...), append([]map[string]any(nil), f.metas...)
}

// Tools satisfies the ToolSource seam the manager field types; the wrap
// exercises Invoke only.
func (f *fakeMetaInvoker) Tools(context.Context, mcp.Ref) ([]tool.BaseTool, error) {
	return nil, nil
}

// stubRawTool carries a fixed raw name so the wrap's name resolution reads it.
type stubRawTool struct{ name string }

func (t *stubRawTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *stubRawTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "{}", nil
}

// newWrapManager wires a manager whose workspace manager roots a temp dir
// with the agent's workspace created, and returns the manager plus the
// workspace root.
func newWrapManager(t *testing.T, slug string) (*manager, string) {
	t.Helper()
	fs := fake.New()
	ws := workspaces.NewManager(t.TempDir())
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithWorkspaces(ws))
	if slug != "" {
		_, err := ws.Create(slug)
		require.NoError(t, err)
	}
	return m, ws.Root()
}

func TestWrapSessionDirToolsStampsTheSessionBrowserDir(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, wsRoot := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	ref := mcp.Ref{ServerID: mcp.SystemServerID, Name: "browser-use"}
	serverTools := []tool.BaseTool{&stubRawTool{name: "screenshot"}, &stubRawTool{name: "navigate"}}
	wrapped := m.wrapSessionDirTools(ref, serverTools, store.Agent{Slug: slug},
		attentionScope{sessionID: sessionID})
	require.Len(t, wrapped, 2)
	for _, wt := range wrapped {
		_, isWrap := wt.(*sessionDirTool)
		require.True(t, isWrap, "every system tool is wrapped, got %T", wt)
	}

	out, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{"profile":"work"}`)
	require.NoError(t, err)
	require.Equal(t, `{"ok":true}`, out)

	names, args, metas := inv.snapshot()
	require.Equal(t, []string{"screenshot"}, names, "the raw name rides the invoke")
	require.Equal(t, []string{`{"profile":"work"}`}, args, "arguments pass through untouched")
	require.Len(t, metas, 1)
	require.Equal(t,
		filepath.Join(wsRoot, slug, "sessions", sessionID, workspaces.DirBrowser),
		metas[0][MetaScreenshotDir],
		"the meta carries the calling session's workspace browser directory")
	require.Equal(t,
		filepath.Join(wsRoot, slug, workspaces.DirBrowserProfile),
		metas[0][MetaProfileDir],
		"browser-use calls carry the agent's workspace profile directory")
	require.Equal(t, slug, metas[0][MetaProfileName],
		"browser-use calls carry the agent slug as the profile display name")
	require.Len(t, metas[0], 3, "the meta carries exactly the screenshots dir and the agent's profile binding")
}

func TestWrapSessionDirToolsDegradesWithoutAScope(t *testing.T) {
	const fullSession = "e7238190a1b2c3d4e5f60718293a4b5c"
	cases := []struct {
		name   string
		inv    metaInvoker
		slug   string
		sessID string
	}{
		{name: "manager without the meta invoker", inv: nil, slug: "scout", sessID: fullSession},
		{name: "agent without a workspace slug", inv: &fakeMetaInvoker{}, slug: "", sessID: fullSession},
		{name: "scope without a session", inv: &fakeMetaInvoker{}, slug: "scout", sessID: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newWrapManager(t, tc.slug)
			if tc.inv != nil {
				m.mcpManager = tc.inv
			}
			serverTools := []tool.BaseTool{&stubRawTool{name: "screenshot"}}
			wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID}, serverTools,
				store.Agent{Slug: tc.slug}, attentionScope{sessionID: tc.sessID})
			require.Equal(t, serverTools, wrapped, "no scope to name a directory with — the tools pass through unwrapped")
		})
	}
}

func TestSessionDirToolPropagatesInvokeErrors(t *testing.T) {
	m, _ := newWrapManager(t, "scout")
	inv := &fakeMetaInvoker{err: errors.New("child is down")}
	m.mcpManager = inv

	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID}, []tool.BaseTool{&stubRawTool{name: "screenshot"}},
		store.Agent{Slug: "scout"}, attentionScope{sessionID: "e7238190a1b2c3d4e5f60718293a4b5c"})
	_, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	require.ErrorContains(t, err, "child is down")
}

// The per-server stamping (add-computer-use coordination contract C1): the
// computer-use server's calls carry the session's computer screenshots
// directory plus the workspace-relative facing path, while browser-use keeps
// stamping exactly what it stamped before — the absolute browser directory,
// no rel key.

func TestWrapSessionDirToolsStampsTheComputerDirForComputerUse(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, wsRoot := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	ref := mcp.Ref{ServerID: computerUseServerID, Name: computerUseServerID}
	wrapped := m.wrapSessionDirTools(ref, []tool.BaseTool{&stubRawTool{name: "observe_window"}},
		store.Agent{Slug: slug}, attentionScope{sessionID: sessionID})
	require.Len(t, wrapped, 1)

	_, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	_, _, metas := inv.snapshot()
	require.Len(t, metas, 1)
	require.Equal(t,
		filepath.Join(wsRoot, slug, "sessions", sessionID, workspaces.DirComputer),
		metas[0][MetaScreenshotDir],
		"the meta carries the calling session's computer directory")
	require.Equal(t,
		workspaces.SessionRel(sessionID, workspaces.DirComputer),
		metas[0][MetaScreenshotRel],
		"the meta carries the workspace-relative facing path")
	require.Len(t, metas[0], 2, "computer-use calls stamp exactly the dir and the rel key")
}

func TestWrapSessionDirToolsKeepsBrowserUseStampUnchanged(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, wsRoot := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID}, []tool.BaseTool{&stubRawTool{name: "screenshot"}},
		store.Agent{Slug: slug}, attentionScope{sessionID: sessionID})
	require.Len(t, wrapped, 1)

	_, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	_, _, metas := inv.snapshot()
	require.Len(t, metas, 1)
	require.Equal(t,
		filepath.Join(wsRoot, slug, "sessions", sessionID, workspaces.DirBrowser),
		metas[0][MetaScreenshotDir],
		"browser-use keeps the session's browser directory")
	require.Equal(t, filepath.Join(wsRoot, slug, workspaces.DirBrowserProfile), metas[0][MetaProfileDir])
	require.Equal(t, slug, metas[0][MetaProfileName])
	require.Len(t, metas[0], 3, "browser-use calls carry no rel key, only the session dir and the profile binding")
}

func TestWrapSessionDirToolsDegradesForComputerUseWithoutScope(t *testing.T) {
	m, _ := newWrapManager(t, "scout")
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	// An invalid session id resolves no computer directory: the tools
	// degrade to unwrapped, like the other scope-less shapes.
	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: computerUseServerID},
		[]tool.BaseTool{&stubRawTool{name: "observe_window"}},
		store.Agent{Slug: "scout"}, attentionScope{sessionID: "not-a-session-id"})
	require.Len(t, wrapped, 1)
	_, isWrap := wrapped[0].(*sessionDirTool)
	require.False(t, isWrap, "no session to name a computer directory with — the tool passes through unwrapped")
}
