package adapter_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"

	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
)

// --- fakes ---

type fakeGeminiCacher struct {
	calls [][]*schema.AgenticMessage
}

func (f *fakeGeminiCacher) CreatePrefixCache(ctx context.Context, prefixMsgs []*schema.AgenticMessage, _ ...model.Option) (*genai.CachedContent, error) {
	f.calls = append(f.calls, prefixMsgs)
	return &genai.CachedContent{Name: "cachedContents/test"}, nil
}

type fakeAgenticModel struct {
	genCalls []genCall
}

type genCall struct {
	n    int
	opts int
}

func (f *fakeAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	f.genCalls = append(f.genCalls, genCall{n: len(input), opts: len(opts)})
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
}

func (f *fakeAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	f.genCalls = append(f.genCalls, genCall{n: len(input), opts: len(opts)})
	return nil, nil
}

func txtMsg(role schema.AgenticRoleType, text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: role,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeUserInputText, UserInputText: &schema.UserInputText{Text: text}},
		},
	}
}

// TestGeminiPrefixCacheCreatesAndReuses asserts the cached-content resource is
// created once from the stable prefix (system + history) and referenced on
// subsequent turns, with the tail (current turn) sent to the inner model.
func TestGeminiPrefixCacheCreatesAndReuses(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1}); err != nil {
		t.Fatalf("turn1 generate: %v", err)
	}
	a1 := txtMsg(schema.AgenticRoleTypeAssistant, "a1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1, a1}); err != nil {
		t.Fatalf("turn2 generate: %v", err)
	}

	if len(cacher.calls) != 1 {
		t.Fatalf("expected 1 CreatePrefixCache call, got %d", len(cacher.calls))
	}
	if len(cacher.calls[0]) != 1 || cacher.calls[0][0].Role != schema.AgenticRoleTypeSystem {
		t.Fatalf("cache prefix should be [sys], got %+v", cacher.calls[0])
	}
	// turn1: inner gets tail [u1] (1 msg) + 1 cached-content opt.
	if inner.genCalls[0].n != 1 || inner.genCalls[0].opts != 1 {
		t.Fatalf("turn1 inner: want n=1 opts=1, got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
	// turn2: inner gets tail [u1, a1] (2 msgs) + 1 cached-content opt; prefix reused.
	if inner.genCalls[1].n != 2 || inner.genCalls[1].opts != 1 {
		t.Fatalf("turn2 inner: want n=2 opts=1, got n=%d opts=%d", inner.genCalls[1].n, inner.genCalls[1].opts)
	}
}

// TestGeminiPrefixCacheDisabledSendsFullInput asserts disabling the flag omits
// the cached-content reference and sends the full input (graceful degradation).
func TestGeminiPrefixCacheDisabledSendsFullInput(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, cacher, false)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(cacher.calls) != 0 {
		t.Fatalf("expected no CreatePrefixCache when disabled, got %d", len(cacher.calls))
	}
	if inner.genCalls[0].n != 2 || inner.genCalls[0].opts != 0 {
		t.Fatalf("disabled: want n=2 opts=0 (full input), got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
}

// TestGeminiPrefixCacheNoHistorySkipsCache asserts a conversation with no
// stable prefix (only a user message) is sent through unchanged, no cache made.
func TestGeminiPrefixCacheNoHistorySkipsCache(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{u1}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(cacher.calls) != 0 {
		t.Fatalf("expected no cache when no history, got %d", len(cacher.calls))
	}
	if inner.genCalls[0].n != 1 || inner.genCalls[0].opts != 0 {
		t.Fatalf("no-history: want n=1 opts=0, got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
}

// TestGeminiPrefixCacheCreateFailureDegrades asserts a cache-creation failure
// falls back to calling the inner model with the full input (no error).
func TestGeminiPrefixCacheCreateFailureDegrades(t *testing.T) {
	ctx := context.Background()
	failing := &failCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, failing, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1}); err != nil {
		t.Fatalf("generate should degrade, got: %v", err)
	}
	// full input sent, no cached-content opt.
	if inner.genCalls[0].n != 2 || inner.genCalls[0].opts != 0 {
		t.Fatalf("degrade: want n=2 opts=0, got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
}

type failCacher struct{}

func (f *failCacher) CreatePrefixCache(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*genai.CachedContent, error) {
	return nil, context.DeadlineExceeded
}

// flakyGeminiModel returns an error on the Nth Generate call (1-indexed) when
// errOnCall is set, so expiry/invalidation and error-propagation paths can be
// exercised. It otherwise behaves like fakeAgenticModel.
type flakyGeminiModel struct {
	calls     []genCall
	errOnCall func(n int) error
}

func (f *flakyGeminiModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	f.calls = append(f.calls, genCall{n: len(input), opts: len(opts)})
	if f.errOnCall != nil {
		if err := f.errOnCall(len(f.calls)); err != nil {
			return nil, err
		}
	}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
}

func (f *flakyGeminiModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	f.calls = append(f.calls, genCall{n: len(input), opts: len(opts)})
	return nil, nil
}

// TestGeminiCacheStaleInvalidatesAndRetries asserts the runtime eviction
// behavior: when the referenced cached content is missing/expired, the wrapper
// invalidates the cached name and transparently retries the full input, so the
// model still answers (re-billed, never incorrect). It also proves the stale
// name was actually cleared — a subsequent same-prefix turn recreates the
// cache resource rather than silently reusing the dead one.
func TestGeminiCacheStaleInvalidatesAndRetries(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &flakyGeminiModel{
		errOnCall: func(n int) error {
			if n == 1 {
				return errors.New("googleapi: Error 404: cachedContent not found")
			}
			return nil
		},
	}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")

	out, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1})
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if out == nil || out.Role != schema.AgenticRoleTypeAssistant {
		t.Fatalf("expected assistant output after retry")
	}
	if len(cacher.calls) != 1 {
		t.Fatalf("expected 1 CreatePrefixCache during turn1, got %d", len(cacher.calls))
	}
	// inner got the cached tail call (opts=1) then the full-input retry (opts=0).
	if len(inner.calls) != 2 {
		t.Fatalf("expected 2 inner Generate calls (cached then retry), got %d", len(inner.calls))
	}
	if inner.calls[0].opts != 1 {
		t.Errorf("cached call should carry the cached-content opt, got opts=%d", inner.calls[0].opts)
	}
	if inner.calls[1].opts != 0 || inner.calls[1].n != 2 {
		t.Errorf("retry should send full input with no cached opt, got n=%d opts=%d", inner.calls[1].n, inner.calls[1].opts)
	}

	// A second same-prefix turn must recreate the cache: invalidate() cleared
	// the name, so ensureCache builds a fresh resource.
	a1 := txtMsg(schema.AgenticRoleTypeAssistant, "a1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1, a1}); err != nil {
		t.Fatalf("turn2 generate: %v", err)
	}
	if len(cacher.calls) != 2 {
		t.Fatalf("expected cache recreation after invalidate, got %d CreatePrefixCache calls", len(cacher.calls))
	}
}

// TestGeminiCacheNonCacheErrorPropagates asserts that only stale-cache errors
// trigger the invalidate+retry; any other error (e.g. rate limit) propagates
// unchanged without a full-input retry.
func TestGeminiCacheNonCacheErrorPropagates(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &flakyGeminiModel{
		errOnCall: func(n int) error {
			if n == 1 {
				return fmt.Errorf("rate limit exceeded")
			}
			return nil
		},
	}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Generate(ctx, []*schema.AgenticMessage{sys, u1}); err == nil {
		t.Fatal("expected non-cache error to propagate (no retry)")
	}
	if len(inner.calls) != 1 {
		t.Fatalf("expected exactly 1 inner call (no retry on non-cache error), got %d", len(inner.calls))
	}
	if len(cacher.calls) != 1 {
		t.Fatalf("expected 1 CreatePrefixCache, got %d", len(cacher.calls))
	}
}

// TestGeminiPrefixCacheGrowsRecreates asserts that when the stable prefix
// deepens (post-compaction or history growth) the cache resource is recreated,
// not reused with a stale hash. turn1 prefix is [sys]; turn2 prefix grows to
// [sys,u1,a1], so CreatePrefixCache is called twice.
func TestGeminiPrefixCacheGrowsRecreates(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	a1 := txtMsg(schema.AgenticRoleTypeAssistant, "a1")
	u2 := txtMsg(schema.AgenticRoleTypeUser, "u2")
	a2 := txtMsg(schema.AgenticRoleTypeAssistant, "a2")

	turn1 := []*schema.AgenticMessage{sys, u1, a1}         // last user = u1 -> prefix [sys]
	turn2 := []*schema.AgenticMessage{sys, u1, a1, u2, a2} // last user = u2 -> prefix [sys,u1,a1]
	if _, err := m.Generate(ctx, turn1); err != nil {
		t.Fatalf("turn1: %v", err)
	}
	if _, err := m.Generate(ctx, turn2); err != nil {
		t.Fatalf("turn2: %v", err)
	}

	if len(cacher.calls) != 2 {
		t.Fatalf("expected 2 CreatePrefixCache calls as prefix grows, got %d", len(cacher.calls))
	}
	if len(cacher.calls[0]) != 1 || cacher.calls[0][0].Role != schema.AgenticRoleTypeSystem {
		t.Errorf("turn1 prefix should be [sys], got %+v", cacher.calls[0])
	}
	if len(cacher.calls[1]) != 3 {
		t.Errorf("turn2 prefix should be [sys,u1,a1], got %+v", cacher.calls[1])
	}
	// turn1 tail [u1,a1] (n=2); turn2 tail [u2,a2] (n=2), both carry cached opt.
	if inner.genCalls[0].n != 2 || inner.genCalls[0].opts != 1 {
		t.Errorf("turn1 inner: want tail n=2 opts=1, got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
	if inner.genCalls[1].n != 2 || inner.genCalls[1].opts != 1 {
		t.Errorf("turn2 inner: want tail n=2 opts=1, got n=%d opts=%d", inner.genCalls[1].n, inner.genCalls[1].opts)
	}
}

// TestGeminiPrefixCacheStreamReuses asserts the Stream path mirrors Generate:
// the cache resource is created once and the tail is sent with the
// cached-content reference.
func TestGeminiPrefixCacheStreamReuses(t *testing.T) {
	ctx := context.Background()
	cacher := &fakeGeminiCacher{}
	inner := &fakeAgenticModel{}
	m := adapter.NewGeminiPrefixCache(inner, cacher, true)

	sys := txtMsg(schema.AgenticRoleTypeSystem, "sys")
	u1 := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := m.Stream(ctx, []*schema.AgenticMessage{sys, u1}); err != nil {
		t.Fatalf("stream turn1: %v", err)
	}
	a1 := txtMsg(schema.AgenticRoleTypeAssistant, "a1")
	if _, err := m.Stream(ctx, []*schema.AgenticMessage{sys, u1, a1}); err != nil {
		t.Fatalf("stream turn2: %v", err)
	}
	if len(cacher.calls) != 1 {
		t.Fatalf("expected 1 CreatePrefixCache for Stream path, got %d", len(cacher.calls))
	}
	if inner.genCalls[0].n != 1 || inner.genCalls[0].opts != 1 {
		t.Errorf("stream turn1 inner: want tail n=1 opts=1, got n=%d opts=%d", inner.genCalls[0].n, inner.genCalls[0].opts)
	}
	if inner.genCalls[1].n != 2 || inner.genCalls[1].opts != 1 {
		t.Errorf("stream turn2 inner: want tail n=2 opts=1, got n=%d opts=%d", inner.genCalls[1].n, inner.genCalls[1].opts)
	}
}

// TestGeminiPrefixCacheStreamDisabledAndNoHistory exercises the Stream
// degraded paths (disabled flag, and no stable prefix) so both early-return
// branches of Stream are covered.
func TestGeminiPrefixCacheStreamDisabledAndNoHistory(t *testing.T) {
	ctx := context.Background()

	// disabled: full input sent, no cache created.
	disabledCacher := &fakeGeminiCacher{}
	disabledInner := &fakeAgenticModel{}
	dm := adapter.NewGeminiPrefixCache(disabledInner, disabledCacher, false)
	u := txtMsg(schema.AgenticRoleTypeUser, "u1")
	if _, err := dm.Stream(ctx, []*schema.AgenticMessage{u}); err != nil {
		t.Fatalf("disabled stream: %v", err)
	}
	if len(disabledCacher.calls) != 0 {
		t.Errorf("disabled: expected no CreatePrefixCache, got %d", len(disabledCacher.calls))
	}
	if disabledInner.genCalls[0].n != 1 || disabledInner.genCalls[0].opts != 0 {
		t.Errorf("disabled stream: want n=1 opts=0, got n=%d opts=%d", disabledInner.genCalls[0].n, disabledInner.genCalls[0].opts)
	}

	// no stable prefix: single user message -> full input, no cache.
	noHistCacher := &fakeGeminiCacher{}
	noHistInner := &fakeAgenticModel{}
	nm := adapter.NewGeminiPrefixCache(noHistInner, noHistCacher, true)
	if _, err := nm.Stream(ctx, []*schema.AgenticMessage{u}); err != nil {
		t.Fatalf("no-history stream: %v", err)
	}
	if len(noHistCacher.calls) != 0 {
		t.Errorf("no-history: expected no CreatePrefixCache, got %d", len(noHistCacher.calls))
	}
	if noHistInner.genCalls[0].n != 1 || noHistInner.genCalls[0].opts != 0 {
		t.Errorf("no-history stream: want n=1 opts=0, got n=%d opts=%d", noHistInner.genCalls[0].n, noHistInner.genCalls[0].opts)
	}
}
