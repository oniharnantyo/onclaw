package agents

import (
	"context"
	"os"
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

// The agent profile binding (add-per-agent-browser-profiles tasks.md 3.1/3.2,
// design D1/D4/D5): browser-use tools — and only browser-use tools — carry
// the agent's workspace profile directory and display name through the call's
// _meta, a binding failure degrades the two keys without dropping the
// session-dir stamping, and the attention channel's reserved calls carry the
// same binding while an unbound channel stays meta-free.

// TestWrapSessionDirToolsStampsTheAgentProfileBindingOnBrowserUse pins the
// binding values at the listing site (task 3.1): the workspace profile
// directory and the agent slug, beside the session's screenshots directory.
func TestWrapSessionDirToolsStampsTheAgentProfileBindingOnBrowserUse(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, wsRoot := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID},
		[]tool.BaseTool{&stubRawTool{name: "navigate"}, &stubRawTool{name: "cookies"}},
		store.Agent{Slug: slug}, attentionScope{sessionID: sessionID})
	require.Len(t, wrapped, 2)
	for _, wt := range wrapped {
		_, isWrap := wt.(*sessionDirTool)
		require.True(t, isWrap, "every browser-use tool is wrapped, got %T", wt)
	}

	_, err := wrapped[1].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	_, _, metas := inv.snapshot()
	require.Len(t, metas, 1)
	require.Equal(t,
		filepath.Join(wsRoot, slug, workspaces.DirBrowserProfile),
		metas[0][MetaProfileDir],
		"the meta carries the agent's workspace profile directory")
	require.Equal(t, slug, metas[0][MetaProfileName],
		"the meta carries the agent slug as the display name")
	require.Len(t, metas[0], 3, "browser-use calls carry the session dir plus the full binding")
}

// TestWrapSessionDirToolsCarriesNoBindingForOtherServers pins the per-server
// gate (task 3.1, design D5): computer-use and any other system server carry
// neither binding key — the binding rides the browser-use surface only.
func TestWrapSessionDirToolsCarriesNoBindingForOtherServers(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, _ := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	for _, serverID := range []string{computerUseServerID, "another-system-server"} {
		wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: serverID, Name: serverID},
			[]tool.BaseTool{&stubRawTool{name: "observe_window"}},
			store.Agent{Slug: slug}, attentionScope{sessionID: sessionID})
		require.Len(t, wrapped, 1, "server %s still wraps the session dir", serverID)

		_, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
		require.NoError(t, err)
		_, _, metas := inv.snapshot()
		require.NotContains(t, metas[len(metas)-1], MetaProfileDir,
			"server %s carries no profile directory", serverID)
		require.NotContains(t, metas[len(metas)-1], MetaProfileName,
			"server %s carries no profile name", serverID)
	}
}

// TestWrapSessionDirToolsDegradesTheProfileBindingOnly pins the degradation
// contract (task 3.1): a profile-binding resolution failure empties the two
// binding keys while the session-dir stamping stays intact — a binding
// failure must never drop the screenshots directory.
func TestWrapSessionDirToolsDegradesTheProfileBindingOnly(t *testing.T) {
	const (
		slug      = "scout"
		sessionID = "e7238190a1b2c3d4e5f60718293a4b5c"
	)
	m, wsRoot := newWrapManager(t, slug)
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	// A regular file occupies the browser-profile path: BrowserProfileDir's
	// mkdir fails, while SessionDir (no filesystem access) still resolves —
	// the exact shape where only the binding degrades.
	blocked := filepath.Join(wsRoot, slug, workspaces.DirBrowserProfile)
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID},
		[]tool.BaseTool{&stubRawTool{name: "screenshot"}},
		store.Agent{Slug: slug}, attentionScope{sessionID: sessionID})
	require.Len(t, wrapped, 1)

	_, err := wrapped[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	require.NoError(t, err)

	_, _, metas := inv.snapshot()
	require.Len(t, metas, 1)
	require.Equal(t,
		filepath.Join(wsRoot, slug, "sessions", sessionID, workspaces.DirBrowser),
		metas[0][MetaScreenshotDir],
		"the session-dir stamping survives the binding failure")
	require.Len(t, metas[0], 1, "the degraded meta carries exactly the screenshots directory")
}

// TestWrapSessionDirToolsDegradesWithoutWorkspacesManager pins the
// nil-workspaces degradation: no workspace manager, no wrap at all (the
// pre-binding contract — the tools pass through unwrapped).
func TestWrapSessionDirToolsDegradesWithoutWorkspacesManager(t *testing.T) {
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
	inv := &fakeMetaInvoker{out: `{"ok":true}`}
	m.mcpManager = inv

	serverTools := []tool.BaseTool{&stubRawTool{name: "screenshot"}}
	wrapped := m.wrapSessionDirTools(mcp.Ref{ServerID: mcp.SystemServerID}, serverTools,
		store.Agent{Slug: "scout"}, attentionScope{sessionID: "e7238190a1b2c3d4e5f60718293a4b5c"})
	require.Equal(t, serverTools, wrapped, "no workspace manager — the tools pass through unwrapped")
}

// metaChildCall is one recorded meta-carrying child invocation.
type metaChildCall struct {
	name string
	args string
	meta map[string]any
}

// metaChildSource is a fake child source with the metaInvoker capability
// (design D4): Invoke answers per raw tool name from a canned reply table
// and records every call — name, arguments, and meta — so tests can assert
// the binding rode the reserved channel.
type metaChildSource struct {
	*stubMCPSource
	entries []mcp.SystemServerEntry

	mu      sync.Mutex
	replies map[string]string // raw tool name -> canned result
	invoked []metaChildCall
}

func (s *metaChildSource) SystemServers() []mcp.SystemServerEntry { return s.entries }

func (s *metaChildSource) Invoke(_ context.Context, ref mcp.Ref, name, argsJSON string, meta map[string]any) (string, error) {
	s.mu.Lock()
	s.invoked = append(s.invoked, metaChildCall{name: name, args: argsJSON, meta: meta})
	reply := s.replies[name]
	s.mu.Unlock()
	if reply == "" {
		return `{"status":"ok"}`, nil
	}
	return reply, nil
}

func (s *metaChildSource) calls() []metaChildCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]metaChildCall(nil), s.invoked...)
}

func newMetaChildSource(replies map[string]string) *metaChildSource {
	return &metaChildSource{
		stubMCPSource: &stubMCPSource{
			tools: map[string][]tool.BaseTool{},
			errs:  map[string]error{},
		},
		entries: []mcp.SystemServerEntry{
			{Ref: mcp.Ref{ServerID: mcp.SystemServerID, Name: mcp.SystemServerID}},
		},
		replies: replies,
	}
}

// resolveScopedSurface resolves the fixture agent's MCP surface with an
// explicit attention scope and returns the model-facing tool named name.
func resolveScopedSurface(t *testing.T, f *fixture, at attentionScope, name string) tool.InvokableTool {
	t.Helper()
	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, at)
	require.NoError(t, err)
	for _, tl := range tools {
		info, ierr := tl.Info(context.Background())
		require.NoError(t, ierr)
		if info.Name == name {
			inv, ok := tl.(tool.InvokableTool)
			require.True(t, ok, "%s must stay invokable through the wrapper", name)
			return inv
		}
	}
	t.Fatalf("tool %s not found in the resolved surface: %v", name, mcpToolNames(t, tools))
	return nil
}

// TestAttentionChannelCarriesTheAgentBinding drives the secret and takeover
// flows over a meta-carrying fake child (task 3.2, design D4): every
// reserved-channel call — __request_secret, __deliver_secret,
// __request_takeover, __begin_takeover, __resolve_attention — reaches the
// child with exactly the agent's binding in its meta, and the takeover card
// names the agent instead of a profile literal.
func TestAttentionChannelCarriesTheAgentBinding(t *testing.T) {
	const (
		profileDir = "/workspaces/scout/browser-profile"
		turnID     = "turn-bind-1"
	)
	binding := map[string]any{MetaProfileDir: profileDir, MetaProfileName: "scout"}

	t.Run("secret flow: request and deliver carry the binding", func(t *testing.T) {
		src := newMetaChildSource(map[string]string{
			childRequestSecret: `{"status":"ready","requestID":"bind-req-1","origin":"https://example.com","method":"password"}`,
			childDeliverSecret: `{"status":"completed"}`,
		})
		src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "submit_secret"}}
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
		}, WithMCPManager(src))
		sess := f.newSession(t)

		wrapper := resolveScopedSurface(t, f, attentionScope{
			sessionID:   sess.ID,
			turnID:      turnID,
			record:      attentionRecord(t, f, sess.ID, turnID),
			profileDir:  profileDir,
			profileName: "scout",
		}, "mcp__browser-use__submit_secret")
		resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"],"method":"password"}`)

		pending := awaitInputRequired(t, f, sess.ID)
		require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "s3cret", "", false, ""))
		res := <-resultCh
		require.NoError(t, res.err)
		require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)

		calls := src.calls()
		require.Len(t, calls, 2)
		require.Equal(t, childRequestSecret, calls[0].name)
		require.Equal(t, `{"targets":["password"],"method":"password"}`, calls[0].args,
			"the model's arguments pass through verbatim")
		require.Equal(t, binding, calls[0].meta, "the prepare call carries exactly the binding")
		require.Equal(t, childDeliverSecret, calls[1].name)
		require.JSONEq(t, `{"requestID":"bind-req-1","value":"s3cret"}`, calls[1].args)
		require.Equal(t, binding, calls[1].meta, "the delivery call carries exactly the binding")
	})

	t.Run("takeover flow: request, begin, and release carry the binding; the card names the agent", func(t *testing.T) {
		src := newMetaChildSource(map[string]string{
			childRequestTakeover:  `{"status":"ready","requestID":"bind-take-1","profile":"work"}`,
			childBeginTakeover:    `{"status":"started"}`,
			childResolveAttention: `{"status":"ok"}`,
		})
		src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "request_takeover"}}
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
		}, WithMCPManager(src))
		sess := f.newSession(t)

		wrapper := resolveScopedSurface(t, f, attentionScope{
			sessionID:   sess.ID,
			turnID:      turnID,
			record:      attentionRecord(t, f, sess.ID, turnID),
			profileDir:  profileDir,
			profileName: "scout",
		}, "mcp__browser-use__request_takeover")
		resultCh := invokeAsync(t.Context(), wrapper, `{"reason":"sign-in-blocked"}`)

		pending := awaitInputRequired(t, f, sess.ID)
		require.Equal(t, sessions.AttentionTakeover, pending.Input.AttentionKind)
		require.Equal(t, "The agent asks to hand over the browser of agent scout for sign-in.",
			pending.Input.Prompt, "the card names the agent, never the child's profile literal")

		require.NoError(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID))
		require.NoError(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, sessions.InputCompleted))
		res := <-resultCh
		require.NoError(t, res.err)
		require.JSONEq(t, `{"status":"completed","next":"take a fresh snapshot"}`, res.out)

		calls := src.calls()
		require.Len(t, calls, 3)
		require.Equal(t, childRequestTakeover, calls[0].name)
		require.Equal(t, `{"reason":"sign-in-blocked"}`, calls[0].args)
		require.Equal(t, binding, calls[0].meta)
		require.Equal(t, childBeginTakeover, calls[1].name)
		require.Equal(t, `{"requestID":"bind-take-1"}`, calls[1].args)
		require.Equal(t, binding, calls[1].meta)
		require.Equal(t, childResolveAttention, calls[2].name)
		require.Equal(t, `{"outcome":"completed","requestID":"bind-take-1"}`, calls[2].args)
		require.Equal(t, binding, calls[2].meta)
	})
}

// TestAttentionUnboundChannelStaysMetaFree pins the unbound contract
// (task 3.2): an agent without a binding rides the plain tool-source path —
// the meta invoker is never consulted, the pre-binding behavior
// byte-identical.
func TestAttentionUnboundChannelStaysMetaFree(t *testing.T) {
	src := newMetaChildSource(map[string]string{
		childRequestSecret: `{"status":"ready","requestID":"unbound-1","origin":"https://example.com"}`,
		childDeliverSecret: `{"status":"completed"}`,
	})
	// The reserved channel is scripted on the plain InvokableTool path: the
	// flow must complete through it — proving the unbound channel never
	// switched to the meta invoker.
	src.tools[mcp.SystemServerID] = []tool.BaseTool{
		&scriptedChildTool{raw: childRequestSecret, reply: staticReply(src.replies[childRequestSecret])},
		&scriptedChildTool{raw: childDeliverSecret, reply: staticReply(src.replies[childDeliverSecret])},
		fakeMCPTool{raw: "submit_secret"},
	}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)
	const turnID = "turn-unbound-1"

	wrapper := resolveScopedSurface(t, f, attentionScope{
		sessionID: sess.ID,
		turnID:    turnID,
		record:    attentionRecord(t, f, sess.ID, turnID),
	}, "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"]}`)

	pending := awaitInputRequired(t, f, sess.ID)
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "s3cret", "", false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)

	require.Empty(t, src.calls(),
		"an unbound channel never rides the meta invoker — the plain tool-source path is unchanged")
}
