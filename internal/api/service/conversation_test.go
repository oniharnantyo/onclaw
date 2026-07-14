package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestService_ListConversations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	list, err := f.svc.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestService_ListMessages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(res.Messages) != 0 {
		t.Errorf("expected empty list, got %d", len(res.Messages))
	}
	if res.CompactionCount != 0 {
		t.Errorf("expected zero compaction count, got %d", res.CompactionCount)
	}
}

func TestService_ListMessages_ListTurnsError(t *testing.T) {
	f := newFixtureWithConv(t, &errConvStore{listTurnsErr: fmt.Errorf("db unavailable")})
	ctx := context.Background()

	if _, err := f.svc.ListMessages(ctx, 1); err == nil {
		t.Fatal("expected error when ListTurns fails")
	}
}

func TestService_ListMessages_CompactionMetaError(t *testing.T) {
	f := newFixtureWithConv(t, &errConvStore{compactionMetaErr: fmt.Errorf("meta unavailable")})
	ctx := context.Background()

	if _, err := f.svc.ListMessages(ctx, 1); err == nil {
		t.Fatal("expected error when GetCompactionMeta fails")
	}
}

// convWithAgent is a conversation store fixture that reports a single
// conversation owned by a known agent, so the context-window resolution path
// can be exercised.
type convWithAgent struct {
	fakeConversationStore
	agentName string
	turns     []*store.TurnRow
}

func (c *convWithAgent) ListConversations(context.Context) ([]*store.ConversationRow, error) {
	return []*store.ConversationRow{{ID: 123, AgentName: c.agentName}}, nil
}

func (c *convWithAgent) ListTurns(_ context.Context, _ int64) ([]*store.TurnRow, error) {
	return c.turns, nil
}

func TestService_ListMessages_ContextWindowFromAgentMax(t *testing.T) {
	conv := &convWithAgent{agentName: "agentA", turns: []*store.TurnRow{{ID: 1, IsSummary: false, PromptTokens: 1234}}}
	f := newFixtureWithConv(t, conv)
	ctx := context.Background()

	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agentA", MaxContextTokens: 5000}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if res.ContextWindow != 5000 {
		t.Errorf("expected context window 5000 from agent MaxContextTokens, got %d", res.ContextWindow)
	}
}

func TestService_ListMessages_ContextWindowAgentOverridesModelMeta(t *testing.T) {
	meta, _ := store.MarshalModelMetadata(&store.ModelMetadata{ContextWindow: 9000})
	conv := &convWithAgent{agentName: "agentA"}
	f := newFixtureWithConv(t, conv)
	ctx := context.Background()

	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agentA", MaxContextTokens: 5000, ModelMetadata: meta}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if res.ContextWindow != 5000 {
		t.Errorf("expected agent MaxContextTokens (5000) to win over model metadata (9000), got %d", res.ContextWindow)
	}
}

func TestService_ListMessages_ContextWindowGlobalDefault(t *testing.T) {
	meta, _ := store.MarshalModelMetadata(&store.ModelMetadata{ContextWindow: 9000})
	conv := &convWithAgent{agentName: "agentA"}
	f := newFixtureWithConv(t, conv)
	ctx := context.Background()

	// Agent has no per-agent override, but a global default is configured.
	f.svc.SetGlobalMaxContextTokens(7000)
	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agentA", ModelMetadata: meta}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if res.ContextWindow != 7000 {
		t.Errorf("expected global default (7000) to win over model metadata (9000), got %d", res.ContextWindow)
	}
}

func TestService_ListMessages_ContextWindowModelMetaData(t *testing.T) {
	meta, _ := store.MarshalModelMetadata(&store.ModelMetadata{ContextWindow: 9000})
	conv := &convWithAgent{agentName: "agentA"}
	f := newFixtureWithConv(t, conv)
	ctx := context.Background()

	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agentA", ModelMetadata: meta}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if res.ContextWindow != 9000 {
		t.Errorf("expected model metadata context window 9000, got %d", res.ContextWindow)
	}
}

func TestService_ListMessages_ContextWindowFallback(t *testing.T) {
	conv := &convWithAgent{agentName: "agentA"}
	f := newFixtureWithConv(t, conv)
	ctx := context.Background()

	if err := f.agentStore.AddAgent(ctx, &store.Agent{Name: "agentA"}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	res, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if res.ContextWindow != 64000 {
		t.Errorf("expected 64000 fallback when no window is configured, got %d", res.ContextWindow)
	}
}
