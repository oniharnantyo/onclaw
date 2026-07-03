package hooks_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/hooks"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type mockHookStore struct {
	mu    sync.Mutex
	hooks map[string]*store.Hook
}

func newMockHookStore() *mockHookStore {
	return &mockHookStore{hooks: make(map[string]*store.Hook)}
}

func (m *mockHookStore) AddHook(ctx context.Context, h *store.Hook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks[h.ID] = h
	return nil
}

func (m *mockHookStore) GetHook(ctx context.Context, id string) (*store.Hook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hooks[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return h, nil
}

func (m *mockHookStore) ListHooks(ctx context.Context) ([]*store.Hook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*store.Hook
	for _, h := range m.hooks {
		list = append(list, h)
	}
	return list, nil
}

func (m *mockHookStore) ListHooksByScopeAndEvent(ctx context.Context, scope string, event string) ([]*store.Hook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*store.Hook
	for _, h := range m.hooks {
		if h.Scope == scope && h.Event == event {
			list = append(list, h)
		}
	}
	return list, nil
}

func (m *mockHookStore) UpdateHook(ctx context.Context, h *store.Hook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks[h.ID] = h
	return nil
}

func (m *mockHookStore) RemoveHook(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.hooks, id)
	return nil
}

func (m *mockHookStore) ToggleHook(ctx context.Context, id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hooks[id]; ok {
		if enabled {
			h.Enabled = 1
		} else {
			h.Enabled = 0
		}
	}
	return nil
}

type mockExecStore struct {
	mu    sync.Mutex
	execs []*store.HookExecution
}

func newMockExecStore() *mockExecStore {
	return &mockExecStore{}
}

func (m *mockExecStore) AppendExecution(ctx context.Context, exec *store.HookExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execs = append(m.execs, exec)
	return nil
}

func (m *mockExecStore) ListExecutions(ctx context.Context) ([]*store.HookExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.execs, nil
}

type mockHandler struct {
	dec   hooks.Decision
	err   error
	delay time.Duration
}

func (mh *mockHandler) Run(ctx context.Context, payload hooks.Payload) (hooks.Decision, error) {
	if mh.delay > 0 {
		select {
		case <-ctx.Done():
			return hooks.DecisionBlock, ctx.Err()
		case <-time.After(mh.delay):
		}
	}
	return mh.dec, mh.err
}

func TestDispatcher_Fire(t *testing.T) {
	// Register mock handler factory
	hooks.Register("mock", func(cfg []byte) (hooks.Handler, error) {
		if string(cfg) == "error" {
			return nil, errors.New("factory error")
		}
		if string(cfg) == "run-error" {
			return &mockHandler{dec: hooks.DecisionBlock, err: errors.New("run error")}, nil
		}
		if string(cfg) == "block" {
			return &mockHandler{dec: hooks.DecisionBlock}, nil
		}
		if string(cfg) == "timeout" {
			return &mockHandler{dec: hooks.DecisionAllow, delay: 50 * time.Millisecond}, nil
		}
		return &mockHandler{dec: hooks.DecisionAllow}, nil
	})

	hs := newMockHookStore()
	es := newMockExecStore()
	d := hooks.NewDispatcher(hs, es)

	ctx := context.Background()

	// 1. Resolution order: Global + Agent hooks sorted by priority desc, created_at asc
	h1 := &store.Hook{ID: "h1", Name: "h1", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Priority: 10, Enabled: 1, CreatedAt: "2026-07-01T00:00:00Z"}
	h2 := &store.Hook{ID: "h2", Name: "h2", Scope: "agent-1", Event: "pre_tool_use", HandlerType: "mock", Priority: 20, Enabled: 1, CreatedAt: "2026-07-01T00:00:01Z"}
	h3 := &store.Hook{ID: "h3", Name: "h3", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Priority: 20, Enabled: 1, CreatedAt: "2026-07-01T00:00:02Z"}

	_ = hs.AddHook(ctx, h1)
	_ = hs.AddHook(ctx, h2)
	_ = hs.AddHook(ctx, h3)

	resolved, err := d.ResolveHooks(ctx, "agent-1", hooks.EventPreToolUse)
	if err != nil {
		t.Fatalf("resolveHooks error: %v", err)
	}

	if len(resolved) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(resolved))
	}
	// Priority sorted: h2 (20, 01Z) before h3 (20, 02Z) before h1 (10, 00Z)
	if resolved[0].ID != "h2" || resolved[1].ID != "h3" || resolved[2].ID != "h1" {
		t.Errorf("incorrect sort order: %s, %s, %s", resolved[0].ID, resolved[1].ID, resolved[2].ID)
	}

	// Reset hook store
	hs.hooks = make(map[string]*store.Hook)

	// 2. Block short-circuit
	blockHook := &store.Hook{ID: "hb", Name: "hb", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "block", Priority: 10, Enabled: 1}
	allowHook := &store.Hook{ID: "ha", Name: "ha", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "allow", Priority: 5, Enabled: 1}
	_ = hs.AddHook(ctx, blockHook)
	_ = hs.AddHook(ctx, allowHook)

	dec, err := d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1"})
	if dec != hooks.DecisionBlock {
		t.Errorf("expected DecisionBlock, got %s", dec)
	}

	// 3. Matcher hit/miss
	hs.hooks = make(map[string]*store.Hook)
	matchHook := &store.Hook{ID: "hm", Name: "hm", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "allow", Matcher: "^exec$", Priority: 10, Enabled: 1}
	_ = hs.AddHook(ctx, matchHook)

	// Miss (should not fire, so allow)
	dec, err = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1", ToolName: "read_file"})
	if dec != hooks.DecisionAllow {
		t.Errorf("expected miss to result in allow, got %s", dec)
	}
	// Hit
	dec, err = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1", ToolName: "exec"})
	if dec != hooks.DecisionAllow {
		t.Errorf("expected hit to result in allow, got %s", dec)
	}

	// 4. Timeout & on_timeout policy
	hs.hooks = make(map[string]*store.Hook)
	timeoutHook := &store.Hook{ID: "ht", Name: "ht", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "timeout", TimeoutMS: 10, OnTimeout: "block", Enabled: 1}
	_ = hs.AddHook(ctx, timeoutHook)

	dec, err = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1"})
	if dec != hooks.DecisionBlock {
		t.Errorf("expected block on timeout, got %s", dec)
	}

	// Change on_timeout to allow
	timeoutHook.OnTimeout = "allow"
	_ = hs.UpdateHook(ctx, timeoutHook)
	dec, err = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1"})
	if dec != hooks.DecisionAllow {
		t.Errorf("expected allow on timeout, got %s", dec)
	}

	// 5. Fail-closed on handler error
	hs.hooks = make(map[string]*store.Hook)
	errHook := &store.Hook{ID: "he", Name: "he", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "run-error", Enabled: 1}
	_ = hs.AddHook(ctx, errHook)
	dec, err = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1"})
	if dec != hooks.DecisionBlock {
		t.Errorf("expected block on error, got %s", dec)
	}

	// 6. Circuit breaker trip
	hs.hooks = make(map[string]*store.Hook)
	tripHook := &store.Hook{ID: "trip", Name: "trip", Scope: "global", Event: "pre_tool_use", HandlerType: "mock", Config: "block", Enabled: 1}
	_ = hs.AddHook(ctx, tripHook)

	// Trigger 5 blocks
	for i := 0; i < 5; i++ {
		_, _ = d.Fire(ctx, hooks.EventPreToolUse, hooks.Payload{Agent: "agent-1"})
	}

	// Verify hook is disabled in store
	gotHook, _ := hs.GetHook(ctx, "trip")
	if gotHook.Enabled != 0 {
		t.Error("expected hook to be disabled by circuit breaker")
	}
}
