package adapter_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
)

// cacheControlExtraKey mirrors agenticclaude's internal key used by
// SetContentBlockCacheControl / SetToolInfoCacheControl to carry cache control
// on a message block or tool info. It is unexported by the library, so the test
// reads it back to confirm the wrapper applied the breakpoint.
const cacheControlExtraKey = "_agenticclaude_cache_control_ttl"

type fakeClaudeModel struct {
	calls []claudeCall
}

type claudeCall struct {
	boundaryHasCache bool
	toolHasCache     bool
}

func (f *fakeClaudeModel) record(input []*schema.AgenticMessage, opts []model.Option) claudeCall {
	c := claudeCall{}
	// Mirror the wrapper: the boundary is the last message before the current
	// user turn. Its final block should carry cache_control.
	cut := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == schema.AgenticRoleTypeUser {
			cut = i
			break
		}
	}
	if cut > 0 {
		last := input[cut-1].ContentBlocks[len(input[cut-1].ContentBlocks)-1]
		if last.Extra != nil {
			if _, ok := last.Extra[cacheControlExtraKey]; ok {
				c.boundaryHasCache = true
			}
		}
	}
	co := model.GetCommonOptions(&model.Options{}, opts...)
	for _, t := range co.Tools {
		if t.Extra != nil {
			if _, ok := t.Extra[cacheControlExtraKey]; ok {
				c.toolHasCache = true
				break
			}
		}
	}
	return c
}

func (f *fakeClaudeModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	f.calls = append(f.calls, f.record(input, opts))
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
}

func (f *fakeClaudeModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	f.calls = append(f.calls, f.record(input, opts))
	return nil, nil
}

func twoTurnMessages() ([]*schema.AgenticMessage, *schema.ToolInfo) {
	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	tool := &schema.ToolInfo{Name: "read_file"}
	return []*schema.AgenticMessage{sys, u1}, tool
}

// TestClaudePrefixCachePinsBoundaryAndTools asserts the wrapper pins a cache
// breakpoint on the boundary message (the system prompt, here the last message
// before the user turn) and on every tool definition.
func TestClaudePrefixCachePinsBoundaryAndTools(t *testing.T) {
	ctx := context.Background()
	inner := &fakeClaudeModel{}
	m := adapter.NewClaudePrefixCache(inner, true)

	msgs, tool := twoTurnMessages()
	if _, err := m.Generate(ctx, msgs, model.WithTools([]*schema.ToolInfo{tool})); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(inner.calls))
	}
	if !inner.calls[0].boundaryHasCache {
		t.Error("expected boundary message (system) to carry cache_control")
	}
	if !inner.calls[0].toolHasCache {
		t.Error("expected tool to carry cache_control")
	}
}

// TestClaudePrefixCacheBoundaryStableAcrossTurns asserts that across turns the
// breakpoint stays at the last history message (not the moving final block):
// turn 2's boundary is the last message before the new user turn.
func TestClaudePrefixCacheBoundaryStableAcrossTurns(t *testing.T) {
	ctx := context.Background()
	inner := &fakeClaudeModel{}
	m := adapter.NewClaudePrefixCache(inner, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	a1 := txtMsg(schema.AgenticRoleTypeAssistant, "a1")
	u2 := txtMsg(schema.AgenticRoleTypeUser, "u2")
	tool := &schema.ToolInfo{Name: "read_file"}

	turn1 := []*schema.AgenticMessage{sys, u1}
	turn2 := []*schema.AgenticMessage{sys, u1, a1, u2}
	for i, msgs := range [][]*schema.AgenticMessage{turn1, turn2} {
		if _, err := m.Generate(ctx, msgs, model.WithTools([]*schema.ToolInfo{tool})); err != nil {
			t.Fatalf("turn%d generate: %v", i+1, err)
		}
	}
	if len(inner.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(inner.calls))
	}
	// turn1 boundary = sys (index 0); turn2 boundary = a1 (index 2, last history msg).
	if !inner.calls[0].boundaryHasCache {
		t.Error("turn1: boundary (system) should carry cache_control")
	}
	if !inner.calls[1].boundaryHasCache {
		t.Error("turn2: boundary (a1, last history msg) should carry cache_control")
	}
	if !inner.calls[0].toolHasCache || !inner.calls[1].toolHasCache {
		t.Error("tools should carry cache_control on every turn")
	}
}

// TestClaudePrefixCacheDisabledNoBreakpoint asserts disabling the flag leaves
// the input untouched (no cache_control applied).
func TestClaudePrefixCacheDisabledNoBreakpoint(t *testing.T) {
	ctx := context.Background()
	inner := &fakeClaudeModel{}
	m := adapter.NewClaudePrefixCache(inner, false)

	msgs, tool := twoTurnMessages()
	if _, err := m.Generate(ctx, msgs, model.WithTools([]*schema.ToolInfo{tool})); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if inner.calls[0].boundaryHasCache {
		t.Error("disabled: boundary should NOT carry cache_control")
	}
	if inner.calls[0].toolHasCache {
		t.Error("disabled: tools should NOT carry cache_control")
	}
}

// TestClaudePrefixCacheStreamPinsBoundaryAndTools asserts the Stream path pins
// the cache breakpoint just like Generate.
func TestClaudePrefixCacheStreamPinsBoundaryAndTools(t *testing.T) {
	ctx := context.Background()
	inner := &fakeClaudeModel{}
	m := adapter.NewClaudePrefixCache(inner, true)

	msgs, tool := twoTurnMessages()
	if _, err := m.Stream(ctx, msgs, model.WithTools([]*schema.ToolInfo{tool})); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("expected 1 stream call, got %d", len(inner.calls))
	}
	if !inner.calls[0].boundaryHasCache {
		t.Error("stream: boundary message (system) should carry cache_control")
	}
	if !inner.calls[0].toolHasCache {
		t.Error("stream: tool should carry cache_control")
	}
}
