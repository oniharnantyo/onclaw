package middlewares_test

import (
	"context"
	"errors"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/hooks"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type mockHookStore struct {
	hooks map[string]*store.Hook
}

func (m *mockHookStore) AddHook(ctx context.Context, h *store.Hook) error { return nil }
func (m *mockHookStore) GetHook(ctx context.Context, id string) (*store.Hook, error) {
	return m.hooks[id], nil
}
func (m *mockHookStore) ListHooks(ctx context.Context) ([]*store.Hook, error) { return nil, nil }
func (m *mockHookStore) ListHooksByScopeAndEvent(ctx context.Context, scope string, event string) ([]*store.Hook, error) {
	var list []*store.Hook
	for _, h := range m.hooks {
		if h.Scope == scope && h.Event == event {
			list = append(list, h)
		}
	}
	return list, nil
}
func (m *mockHookStore) UpdateHook(ctx context.Context, h *store.Hook) error           { return nil }
func (m *mockHookStore) RemoveHook(ctx context.Context, id string) error               { return nil }
func (m *mockHookStore) ToggleHook(ctx context.Context, id string, enabled bool) error { return nil }

type mockExecStore struct{}

func (m *mockExecStore) AppendExecution(ctx context.Context, exec *store.HookExecution) error {
	return nil
}
func (m *mockExecStore) ListExecutions(ctx context.Context) ([]*store.HookExecution, error) {
	return nil, nil
}

func TestHooksMiddleware(t *testing.T) {
	// Register a mock handler in the hooks registry
	hooks.Register("test-mw", func(cfg []byte) (hooks.Handler, error) {
		return &mockMWHandler{cfg: string(cfg)}, nil
	})

	hs := &mockHookStore{hooks: make(map[string]*store.Hook)}
	es := &mockExecStore{}
	d := hooks.NewDispatcher(hs, es)

	mw := middlewares.NewHooksMiddleware(d, "test-agent", &middlewares.SessionState{Channel: "cli", SessionID: "session-123"})

	ctx := context.Background()

	t.Run("user_prompt_submit allow", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "user_prompt_submit", HandlerType: "test-mw", Config: "allow", Enabled: 1},
		}

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		var err error
		_, runCtx, err = mw.BeforeAgent(ctx, runCtx)
		if err != nil {
			t.Fatalf("BeforeAgent failed: %v", err)
		}

		if !strings.Contains(runCtx.AgentInput.Messages[0].String(), "Hello") {
			t.Errorf("expected original message 'Hello', got %s", runCtx.AgentInput.Messages[0].String())
		}
	})

	t.Run("user_prompt_submit block", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "user_prompt_submit", HandlerType: "test-mw", Config: "block", Enabled: 1},
		}

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		var err error
		_, runCtx, err = mw.BeforeAgent(ctx, runCtx)
		if err != nil {
			t.Fatalf("BeforeAgent failed: %v", err)
		}

		msgStr := runCtx.AgentInput.Messages[0].String()
		if !strings.Contains(msgStr, "[Blocked]") {
			t.Errorf("expected blocked message, got %s", msgStr)
		}
	})

	t.Run("pre_tool_use block (invokable)", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "block", Enabled: 1},
		}

		tCtx := &adk.ToolContext{Name: "my_tool", CallID: "call_1"}
		origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
			return "original result", nil
		}

		wrapped, err := mw.WrapInvokableToolCall(ctx, origEndpoint, tCtx)
		if err != nil {
			t.Fatalf("WrapInvokableToolCall failed: %v", err)
		}

		res, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("endpoint run failed: %v", err)
		}

		if !strings.Contains(res, "Tool call blocked") {
			t.Errorf("expected blocked tool result, got %s", res)
		}
	})
}

type mockMWHandler struct {
	cfg string
}

func (m *mockMWHandler) Run(ctx context.Context, payload hooks.Payload) (hooks.Decision, error) {
	if m.cfg == "block" {
		return hooks.DecisionBlock, errors.New("blocked by test")
	}
	return hooks.DecisionAllow, nil
}

func TestHooksMiddleware_EmptyBeforeAgent(t *testing.T) {
	hs := &mockHookStore{hooks: make(map[string]*store.Hook)}
	es := &mockExecStore{}
	d := hooks.NewDispatcher(hs, es)

	mw := middlewares.NewHooksMiddleware(d, "test-agent", &middlewares.SessionState{Channel: "cli", SessionID: "session-123"})

	ctx := context.Background()
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: nil,
		},
	}

	_, runCtx, err := mw.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}
	if len(runCtx.AgentInput.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(runCtx.AgentInput.Messages))
	}

	// Block case with empty messages
	hs.hooks = map[string]*store.Hook{
		"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "user_prompt_submit", HandlerType: "test-mw", Config: "block", Enabled: 1},
	}
	_, runCtx2, err := mw.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}
	if len(runCtx2.AgentInput.Messages) != 1 || !strings.Contains(runCtx2.AgentInput.Messages[0].String(), "[Blocked]") {
		t.Errorf("expected 1 blocked message, got %v", runCtx2.AgentInput.Messages)
	}
}

func TestHooksMiddleware_AfterAgent(t *testing.T) {
	hs := &mockHookStore{hooks: make(map[string]*store.Hook)}
	es := &mockExecStore{}
	d := hooks.NewDispatcher(hs, es)

	mw := middlewares.NewHooksMiddleware(d, "test-agent", &middlewares.SessionState{Channel: "cli", SessionID: "session-123"})
	_, err := mw.AfterAgent(context.Background(), nil)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}
}

func TestHooksMiddleware_WrapStreamableToolCall(t *testing.T) {
	hs := &mockHookStore{hooks: make(map[string]*store.Hook)}
	es := &mockExecStore{}
	d := hooks.NewDispatcher(hs, es)

	mw := middlewares.NewHooksMiddleware(d, "test-agent", &middlewares.SessionState{Channel: "cli", SessionID: "session-123"})

	ctx := context.Background()

	t.Run("streamable block", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "block", Enabled: 1},
		}

		tCtx := &adk.ToolContext{Name: "my_stream_tool", CallID: "call_1"}
		origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, errors.New("should not be called")
		}

		wrapped, err := mw.WrapStreamableToolCall(ctx, origEndpoint, tCtx)
		if err != nil {
			t.Fatalf("WrapStreamableToolCall failed: %v", err)
		}

		sr, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("wrapped endpoint run failed: %v", err)
		}
		defer sr.Close()

		msg, err := sr.Recv()
		if err != nil {
			t.Fatalf("recv failed: %v", err)
		}
		if !strings.Contains(msg, "Tool call blocked") {
			t.Errorf("expected block message, got %s", msg)
		}
	})

	t.Run("streamable allow success", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "allow", Enabled: 1},
		}

		tCtx := &adk.ToolContext{Name: "my_stream_tool", CallID: "call_1"}
		origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
			sr, sw := schema.Pipe[string](2)
			sw.Send("hello ", nil)
			sw.Send("world", nil)
			sw.Close()
			return sr, nil
		}

		wrapped, err := mw.WrapStreamableToolCall(ctx, origEndpoint, tCtx)
		if err != nil {
			t.Fatalf("WrapStreamableToolCall failed: %v", err)
		}

		sr, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("wrapped endpoint run failed: %v", err)
		}
		defer sr.Close()

		var res []string
		for {
			chunk, err := sr.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("unexpected read err: %v", err)
			}
			res = append(res, chunk)
		}

		fullText := strings.Join(res, "")
		if fullText != "hello world" {
			t.Errorf("expected 'hello world', got %q", fullText)
		}
	})

	t.Run("streamable endpoint error", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "allow", Enabled: 1},
		}

		tCtx := &adk.ToolContext{Name: "my_stream_tool", CallID: "call_1"}
		origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, errors.New("endpoint failure")
		}

		wrapped, err := mw.WrapStreamableToolCall(ctx, origEndpoint, tCtx)
		if err != nil {
			t.Fatalf("WrapStreamableToolCall failed: %v", err)
		}

		_, err = wrapped(ctx, "{}")
		if err == nil {
			t.Fatal("expected endpoint error, got nil")
		}
	})

	t.Run("streamable mid-stream error", func(t *testing.T) {
		hs.hooks = map[string]*store.Hook{
			"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "allow", Enabled: 1},
		}

		tCtx := &adk.ToolContext{Name: "my_stream_tool", CallID: "call_1"}
		origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
			sr, sw := schema.Pipe[string](2)
			sw.Send("part1", nil)
			sw.Send("", errors.New("stream interrupted"))
			sw.Close()
			return sr, nil
		}

		wrapped, err := mw.WrapStreamableToolCall(ctx, origEndpoint, tCtx)
		if err != nil {
			t.Fatalf("WrapStreamableToolCall failed: %v", err)
		}

		sr, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("wrapped endpoint run failed: %v", err)
		}
		defer sr.Close()

		chunk1, err := sr.Recv()
		if err != nil {
			t.Fatalf("recv 1 failed: %v", err)
		}
		if chunk1 != "part1" {
			t.Errorf("expected part1, got %s", chunk1)
		}

		_, err = sr.Recv()
		if err == nil || !strings.Contains(err.Error(), "stream interrupted") {
			t.Errorf("expected stream interrupted error, got %v", err)
		}
	})
}

func TestHooksMiddleware_WrapInvokableToolCall_Error(t *testing.T) {
	hs := &mockHookStore{hooks: make(map[string]*store.Hook)}
	es := &mockExecStore{}
	d := hooks.NewDispatcher(hs, es)

	mw := middlewares.NewHooksMiddleware(d, "test-agent", &middlewares.SessionState{Channel: "cli", SessionID: "session-123"})

	ctx := context.Background()

	hs.hooks = map[string]*store.Hook{
		"h1": {ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "test-mw", Config: "allow", Enabled: 1},
	}

	tCtx := &adk.ToolContext{Name: "my_tool", CallID: "call_1"}
	origEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		return "", errors.New("execution error")
	}

	wrapped, err := mw.WrapInvokableToolCall(ctx, origEndpoint, tCtx)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall failed: %v", err)
	}

	_, err = wrapped(ctx, "{}")
	if err == nil {
		t.Fatal("expected execution error, got nil")
	}
}
