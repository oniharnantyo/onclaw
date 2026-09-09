package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// blockedToolHook builds a workspace pre_tool_use hook whose command handler
// exits 2 with the given stderr reason — the canonical block shape.
func blockedToolHook(t *testing.T, name string, tools []string) *domain.WorkspaceHook {
	t.Helper()
	return &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        name,
			Event:       domain.HookEventPreToolUse,
			Matcher:     strings.Join(tools, "|"),
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo blocked by policy >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
}

func mustHookJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// resolveHookChain seeds the workspace (plus the optional hook) in a fake
// store and resolves the per-run chain over it.
func resolveHookChain(t *testing.T, hook *domain.WorkspaceHook) (store.Store, *domain.Workspace, *agenthooks.Resolved) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if hook != nil {
		hook.WorkspaceID = ws.ID
		if err := st.Hooks().CreateWorkspaceHook(ctx, hook); err != nil {
			t.Fatalf("create hook: %v", err)
		}
	}
	chain, err := agenthooks.NewDispatcher(st.Hooks(), agenthooks.NewRegistry()).Resolve(ctx, ws.ID, "agent-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return st, ws, chain
}

// recorderEndpoint records whether it ran and returns a canned result.
type recorderEndpoint struct {
	mu  sync.Mutex
	ran bool
	out string
	err error
}

func (r *recorderEndpoint) call(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = true
	return r.out, r.err
}

func (r *recorderEndpoint) didRun(t *testing.T) bool {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ran
}

func TestHooksMiddleware_InvokableBlockShortCircuitsEndpoint(t *testing.T) {
	_, _, chain := resolveHookChain(t, blockedToolHook(t, "policy-guard", []string{"read_file"}))
	mw := newHooksMiddleware(chain, agenthooks.Event{})

	endpoint := &recorderEndpoint{}
	wrapped, err := mw.WrapInvokableToolCall(context.Background(), endpoint.call, &adk.ToolContext{Name: "read_file", CallID: "call-1"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	out, err := wrapped(context.Background(), `{"path":"/tmp/x"}`)
	if err != nil {
		t.Fatalf("blocked call must not error, got %v", err)
	}
	if endpoint.didRun(t) {
		t.Fatal("blocked call must never reach the tool")
	}
	want := agenthooks.BlockToolResult("policy-guard", "blocked by policy")
	if out != want {
		t.Fatalf("block result = %q, want canonical %q", out, want)
	}
}

func TestHooksMiddleware_StreamableBlockShortCircuitsEndpoint(t *testing.T) {
	_, _, chain := resolveHookChain(t, blockedToolHook(t, "policy-guard", []string{"read_file"}))
	mw := newHooksMiddleware(chain, agenthooks.Event{})

	called := false
	wrapped, err := mw.WrapStreamableToolCall(context.Background(),
		func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
			called = true
			return nil, errors.New("must not run")
		}, &adk.ToolContext{Name: "read_file", CallID: "call-1"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	sr, err := wrapped(context.Background(), `{"path":"/tmp/x"}`)
	if err != nil {
		t.Fatalf("blocked call must not error, got %v", err)
	}
	if called {
		t.Fatal("blocked call must never reach the tool")
	}
	var sb strings.Builder
	for {
		chunk, rerr := sr.Recv()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("recv: %v", rerr)
		}
		sb.WriteString(chunk)
	}
	if got := sb.String(); got != agenthooks.BlockToolResult("policy-guard", "blocked by policy") {
		t.Fatalf("block stream = %q, want canonical block JSON", got)
	}
}

func TestHooksMiddleware_InterruptAndCancelPassThrough(t *testing.T) {
	// A hook whose matcher selects a DIFFERENT tool leaves this call alone;
	// the endpoint's interrupt and cancellation errors must reach the ADK
	// untouched — they are the approval flow's control channel.
	_, _, chain := resolveHookChain(t, blockedToolHook(t, "policy-guard", []string{"write_file"}))
	mw := newHooksMiddleware(chain, agenthooks.Event{})
	ctx := context.Background()

	interruptErr := error(&adk.InterruptSignal{})
	cancelErr := error(context.Canceled)

	invInterrupt := &recorderEndpoint{err: interruptErr}
	invCancel := &recorderEndpoint{err: cancelErr}
	wrappedI, err := mw.WrapInvokableToolCall(ctx, invInterrupt.call, &adk.ToolContext{Name: "read_file", CallID: "c1"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	wrappedC, err := mw.WrapInvokableToolCall(ctx, invCancel.call, &adk.ToolContext{Name: "read_file", CallID: "c2"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if _, err := wrappedI(ctx, `{}`); !errors.Is(err, interruptErr) {
		t.Fatalf("interrupt error altered: %v", err)
	}
	if _, err := wrappedC(ctx, `{}`); !errors.Is(err, cancelErr) {
		t.Fatalf("cancel error altered: %v", err)
	}

	streamErr := func(err error) func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		return func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, err
		}
	}
	wsI, err := mw.WrapStreamableToolCall(ctx, streamErr(interruptErr), &adk.ToolContext{Name: "read_file", CallID: "c3"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	wsC, err := mw.WrapStreamableToolCall(ctx, streamErr(cancelErr), &adk.ToolContext{Name: "read_file", CallID: "c4"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if _, err := wsI(ctx, `{}`); !errors.Is(err, interruptErr) {
		t.Fatalf("streamable interrupt error altered: %v", err)
	}
	if _, err := wsC(ctx, `{}`); !errors.Is(err, cancelErr) {
		t.Fatalf("streamable cancel error altered: %v", err)
	}
}

func TestHooksMiddleware_PostToolUseFiresDetached(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	hook := &domain.WorkspaceHook{
		WorkspaceID: ws.ID,
		HookBase: domain.HookBase{
			Name:        "post-observer",
			Event:       domain.HookEventPostToolUse,
			Matcher:     "read_file",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "true"}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	if err := st.Hooks().CreateWorkspaceHook(ctx, hook); err != nil {
		t.Fatalf("create hook: %v", err)
	}
	chain, err := agenthooks.NewDispatcher(st.Hooks(), agenthooks.NewRegistry()).Resolve(ctx, ws.ID, "agent-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	mw := newHooksMiddleware(chain, agenthooks.Event{Workspace: agenthooks.EventRef{ID: ws.ID}})

	// Invokable flavor: success fires the detached observer.
	inv := &recorderEndpoint{out: "file contents"}
	wrapped, err := mw.WrapInvokableToolCall(ctx, inv.call, &adk.ToolContext{Name: "read_file", CallID: "c1"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if _, err := wrapped(ctx, `{}`); err != nil {
		t.Fatalf("call: %v", err)
	}

	// Streamable flavor: the stream forwards untouched (the wrapper never
	// touches frames) and the observer fires.
	swrapped, err := mw.WrapStreamableToolCall(ctx, func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		sr, sender := schema.Pipe[string](1)
		_ = sender.Send("chunk", nil)
		sender.Close()
		return sr, nil
	}, &adk.ToolContext{Name: "read_file", CallID: "c2"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	sr, err := swrapped(ctx, `{}`)
	if err != nil {
		t.Fatalf("streamable call: %v", err)
	}
	var got strings.Builder
	for {
		chunk, rerr := sr.Recv()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("recv: %v", rerr)
		}
		got.WriteString(chunk)
	}
	if got.String() != "chunk" {
		t.Fatalf("stream frames altered: %q", got.String())
	}

	// Both deliveries land as audit rows without blocking the call.
	deadline := time.Now().Add(3 * time.Second)
	for {
		execs, err := st.Hooks().ListHookExecutions(ctx, ws.ID, nil, 10)
		if err != nil {
			t.Fatalf("list executions: %v", err)
		}
		if len(execs) >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 post_tool_use deliveries, got %d", len(execs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
