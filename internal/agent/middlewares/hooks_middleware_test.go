package middlewares

import (
	"context"
	"errors"
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

	mw := NewHooksMiddleware(d, "test-agent", &SessionState{Channel: "cli", SessionID: "session-123"})

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
