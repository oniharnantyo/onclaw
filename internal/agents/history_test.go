package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func setupHistoryTest(t *testing.T) (store.Store, *Runner, context.Context) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()
	runner := NewRunner(
		st.Workspaces(),
		st.Agents(),
		st.Users(),
		st.Members(),
		st.Roles(),
		st.Providers(),
		st.SessionEvents(),
		st.SessionCheckpoints(),
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
	)
	return st, runner, ctx
}

func createTestWorkspaceAndAgent(t *testing.T, ctx context.Context, st store.Store, wsSlug, agSlug string) (*domain.Workspace, *domain.Agent) {
	t.Helper()
	ws := &domain.Workspace{
		Slug: wsSlug,
		Name: wsSlug,
	}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	prov := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "Test OpenAI",
	}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        agSlug,
		Name:        agSlug,
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws, ag
}

func TestHistory_Validation(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, ag := createTestWorkspaceAndAgent(t, ctx, st, "ws-val", "ag-val")

	t.Run("missing workspace_id", func(t *testing.T) {
		_, err := agent.History(ctx, HistoryRequest{
			SessionID: "sess-1",
		})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid, got %v", err)
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		_, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
		})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid, got %v", err)
		}
	})

	t.Run("agent_id nonexistent in workspace", func(t *testing.T) {
		_, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			AgentID:     "nonexistent-ag",
			SessionID:   "sess-1",
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected domain.ErrNotFound, got %v", err)
		}
	})

	t.Run("agent_id in other workspace rejected", func(t *testing.T) {
		ws2, ag2 := createTestWorkspaceAndAgent(t, ctx, st, "ws-val-2", "ag-val-2")
		_ = ws2
		_, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			AgentID:     ag2.ID,
			SessionID:   "sess-1",
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected domain.ErrNotFound, got %v", err)
		}
	})

	t.Run("valid agent_id succeeds", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			AgentID:     ag.ID,
			SessionID:   "sess-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil || len(res.Events) != 0 {
			t.Fatalf("expected empty result, got %v", res)
		}
	})
}

func TestHistory_RoundTrip(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, ag := createTestWorkspaceAndAgent(t, ctx, st, "ws-rt", "ag-rt")
	sessionID := "sess-rt-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	t1 := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 4, 10, 0, 1, 0, time.UTC)
	t3 := time.Date(2026, 9, 4, 10, 0, 2, 0, time.UTC)
	t4 := time.Date(2026, 9, 4, 10, 0, 3, 0, time.UTC)
	t5 := time.Date(2026, 9, 4, 10, 0, 4, 0, time.UTC)
	t6 := time.Date(2026, 9, 4, 10, 0, 5, 0, time.UTC)
	tIgnored := time.Date(2026, 9, 4, 9, 59, 59, 0, time.UTC)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		// 0. Ignored lifecycle event
		{
			EventID:   "evt-0",
			TurnID:    "turn-1",
			Timestamp: tIgnored,
			Kind:      adk.SessionEventSessionStatusRunning,
			Lifecycle: &adk.LifecycleEvent{State: "running"},
		},
		// 1. User message
		{
			EventID:   "evt-1",
			TurnID:    "turn-1",
			Timestamp: t1,
			Message:   schema.UserAgenticMessage("Calculate 6 * 7"),
		},
		// 2. Tool call start
		{
			EventID:   "evt-2",
			TurnID:    "turn-1",
			Timestamp: t2,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: t2,
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-calc-42",
					Name:      "calculator",
				},
			},
		},
		// 3. Tool call end
		{
			EventID:   "evt-3",
			TurnID:    "turn-1",
			Timestamp: t3,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindTool,
				EndedAt: t3,
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-calc-42",
					Name:      "calculator",
				},
			},
		},
		// 4. Messages replaced (compaction)
		{
			EventID:          "evt-4",
			TurnID:           "turn-1",
			Timestamp:        t4,
			Kind:             adk.SessionEventMessagesReplaced,
			MessagesReplaced: &[]*schema.AgenticMessage{},
		},
		// 5. Assistant message
		{
			EventID:   "evt-5",
			TurnID:    "turn-1",
			Timestamp: t5,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "The answer is 42."}),
				},
			},
		},
		// 6. Model span end with provider usage (not translated directly; feeds
		// the terminal turn_completed usage).
		{
			EventID:   "evt-5b",
			TurnID:    "turn-1",
			Timestamp: t6,
			Kind:      adk.SessionEventSpanModelRequestEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindModel,
				EndedAt: t6,
				Model: &adk.ModelSpanMeta{
					Usage: &adk.ModelUsage{
						InputTokens:  30,
						OutputTokens: 20,
						Raw:          &schema.TokenUsage{PromptTokens: 30, CompletionTokens: 20, TotalTokens: 50},
					},
				},
			},
		},
		// 7. Cancel marker
		{
			EventID:   "evt-6",
			TurnID:    "turn-1",
			Timestamp: t6,
			Kind:      adk.SessionEventCancel,
			Cancel:    &adk.CancelEvent{Reason: "user cancelled"},
		},
	}

	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("agent.History failed: %v", err)
	}

	if res.Next != "" {
		t.Fatalf("expected empty Next on unpaginated query, got %q", res.Next)
	}

	if len(res.Events) != 7 {
		t.Fatalf("expected 7 translated events (excluding ignored evt-0), got %d", len(res.Events))
	}

	// 1. User message completed
	e1 := res.Events[0]
	if e1.ID != "evt-1" || e1.Kind != TranscriptEventMessageCompleted || e1.TurnID != "turn-1" {
		t.Errorf("e1 mismatch: %+v", e1)
	}
	if !e1.OccurredAt.Equal(t1) {
		t.Errorf("e1 OccurredAt got %v, want %v", e1.OccurredAt, t1)
	}
	if e1.Message == nil || e1.Message.Role != "user" || e1.Message.Content != "Calculate 6 * 7" {
		t.Errorf("e1 Message mismatch: %+v", e1.Message)
	}

	// 2. Tool call started
	e2 := res.Events[1]
	if e2.ID != "evt-2" || e2.Kind != TranscriptEventToolCallStarted || e2.TurnID != "turn-1" {
		t.Errorf("e2 mismatch: %+v", e2)
	}
	if !e2.OccurredAt.Equal(t2) {
		t.Errorf("e2 OccurredAt got %v, want %v", e2.OccurredAt, t2)
	}
	if e2.ToolCall == nil || e2.ToolCall.CallID != "call-calc-42" || e2.ToolCall.Name != "calculator" {
		t.Errorf("e2 ToolCall mismatch: %+v", e2.ToolCall)
	}

	// 3. Tool call finished
	e3 := res.Events[2]
	if e3.ID != "evt-3" || e3.Kind != TranscriptEventToolCallFinished || e3.TurnID != "turn-1" {
		t.Errorf("e3 mismatch: %+v", e3)
	}
	if !e3.OccurredAt.Equal(t3) {
		t.Errorf("e3 OccurredAt got %v, want %v", e3.OccurredAt, t3)
	}
	if e3.ToolResult == nil || e3.ToolResult.CallID != "call-calc-42" || e3.ToolResult.Name != "calculator" {
		t.Errorf("e3 ToolResult mismatch: %+v", e3.ToolResult)
	}

	// 4. Compaction
	e4 := res.Events[3]
	if e4.ID != "evt-4" || e4.Kind != TranscriptEventContextCompacted || e4.TurnID != "turn-1" {
		t.Errorf("e4 mismatch: %+v", e4)
	}
	if !e4.OccurredAt.Equal(t4) {
		t.Errorf("e4 OccurredAt got %v, want %v", e4.OccurredAt, t4)
	}
	if e4.Compaction == nil {
		t.Errorf("e4 Compaction is nil")
	}

	// 5. Assistant message completed
	e5 := res.Events[4]
	if e5.ID != "evt-5" || e5.Kind != TranscriptEventMessageCompleted || e5.TurnID != "turn-1" {
		t.Errorf("e5 mismatch: %+v", e5)
	}
	if !e5.OccurredAt.Equal(t5) {
		t.Errorf("e5 OccurredAt got %v, want %v", e5.OccurredAt, t5)
	}
	if e5.Message == nil || e5.Message.Role != "assistant" || e5.Message.Content != "The answer is 42." {
		t.Errorf("e5 Message mismatch: %+v", e5.Message)
	}

	// 6. Cancel marker
	e6 := res.Events[5]
	if e6.ID != "evt-6" || e6.Kind != TranscriptEventCancelled || e6.TurnID != "turn-1" {
		t.Errorf("e6 mismatch: %+v", e6)
	}
	if !e6.OccurredAt.Equal(t6) {
		t.Errorf("e6 OccurredAt got %v, want %v", e6.OccurredAt, t6)
	}
	if e6.CancelReason != "user cancelled" {
		t.Errorf("e6 CancelReason got %q, want %q", e6.CancelReason, "user cancelled")
	}

	// 7. Terminal turn_completed with the turn's accumulated provider usage.
	e7 := res.Events[6]
	if e7.Kind != TranscriptEventTurnCompleted || e7.TurnID != "turn-1" {
		t.Errorf("e7 mismatch: %+v", e7)
	}
	if e7.Usage == nil || e7.Usage.InputTokens != 30 || e7.Usage.OutputTokens != 20 || e7.Usage.TotalTokens != 50 {
		t.Errorf("e7 Usage mismatch: %+v", e7.Usage)
	}
}

// TestHistory_RebuildFinalCallInput verifies a persisted multi-call turn
// rebuilds to the live semantics: summed input totals unchanged, and
// final-call input equal to the last model call's input.
func TestHistory_RebuildFinalCallInput(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, ag := createTestWorkspaceAndAgent(t, ctx, st, "ws-final-in", "ag-final-in")
	sessionID := "sess-final-in-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	t1 := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 5, 8, 0, 1, 0, time.UTC)
	t3 := time.Date(2026, 9, 5, 8, 0, 2, 0, time.UTC)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		{
			EventID:   "fi-user",
			TurnID:    "turn-fi",
			Timestamp: t1,
			Message:   schema.UserAgenticMessage("two calls please"),
		},
		{
			EventID:   "fi-span-1",
			TurnID:    "turn-fi",
			Timestamp: t2,
			Kind:      adk.SessionEventSpanModelRequestEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindModel,
				EndedAt: t2,
				Model: &adk.ModelSpanMeta{
					Usage: &adk.ModelUsage{
						InputTokens:  85000,
						OutputTokens: 1000,
						Raw:          &schema.TokenUsage{PromptTokens: 85000, CompletionTokens: 1000, TotalTokens: 86000},
					},
				},
			},
		},
		{
			EventID:   "fi-span-2",
			TurnID:    "turn-fi",
			Timestamp: t3,
			Kind:      adk.SessionEventSpanModelRequestEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindModel,
				EndedAt: t3,
				Model: &adk.ModelSpanMeta{
					Usage: &adk.ModelUsage{
						InputTokens:  50000,
						OutputTokens: 2000,
						Raw:          &schema.TokenUsage{PromptTokens: 50000, CompletionTokens: 2000, TotalTokens: 52000},
					},
				},
			},
		},
		// Trailing assistant message: projects the turn's closing event so the
		// rebuilt turn terminates; its row carries no usage of its own.
		{
			EventID:   "fi-assistant",
			TurnID:    "turn-fi",
			Timestamp: t3,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "Done."}),
				},
			},
		},
	}

	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("agent.History failed: %v", err)
	}

	var terminal *TranscriptEvent
	for i := range res.Events {
		if res.Events[i].Kind == TranscriptEventTurnCompleted {
			terminal = &res.Events[i]
		}
	}
	if terminal == nil || terminal.Usage == nil {
		t.Fatalf("terminal = %+v, want usage-bearing turn_completed", terminal)
	}
	// Rebuilt sums match the live stream, and final-call input is the last
	// call's input, not the turn total.
	if terminal.Usage.InputTokens != 135000 || terminal.Usage.OutputTokens != 3000 || terminal.Usage.TotalTokens != 138000 {
		t.Fatalf("usage = %+v, want in=135000 out=3000 total=138000", terminal.Usage)
	}
	if terminal.Usage.FinalInputTokens != 50000 {
		t.Fatalf("final input = %d, want 50000", terminal.Usage.FinalInputTokens)
	}
}

func TestHistory_Interrupt(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-intr", "ag-intr")
	sessionID := "sess-intr-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		{
			EventID: "evt-intr",
			TurnID:  "turn-intr",
			Kind:    adk.SessionEventInterrupt,
			Interrupt: &adk.InterruptEvent{
				Contexts: []*adk.InterruptContext{
					{InterruptID: "agent:test;tool:approval"},
				},
			},
		},
	}
	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("agent.History failed: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if res.Events[0].Kind != TranscriptEventApprovalRequired {
		t.Errorf("expected TranscriptEventApprovalRequired, got %v", res.Events[0].Kind)
	}
	if res.Events[0].Approval == nil || res.Events[0].Approval.InterruptID != "agent:test;tool:approval" {
		t.Errorf("Approval payload got %+v, want interrupt_id %q", res.Events[0].Approval, "agent:test;tool:approval")
	}
}

func TestHistory_Pagination(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-page", "ag-page")
	sessionID := "sess-page-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "msg-1", Message: schema.UserAgenticMessage("msg 1")},
		{EventID: "msg-2", Message: schema.UserAgenticMessage("msg 2")},
		{EventID: "msg-3", Message: schema.UserAgenticMessage("msg 3")},
		{EventID: "msg-4", Message: schema.UserAgenticMessage("msg 4")},
		{EventID: "msg-5", Message: schema.UserAgenticMessage("msg 5")},
	}
	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// Page 1: limit 2
	p1, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("page 1 failed: %v", err)
	}
	if len(p1.Events) != 2 {
		t.Fatalf("page 1 expected 2 events, got %d", len(p1.Events))
	}
	if p1.Events[0].ID != "msg-1" || p1.Events[1].ID != "msg-2" {
		t.Errorf("page 1 IDs unexpected: %s, %s", p1.Events[0].ID, p1.Events[1].ID)
	}
	if p1.Next != "msg-2" {
		t.Errorf("page 1 Next got %q, want %q", p1.Next, "msg-2")
	}

	// Page 2: after msg-2, limit 2
	p2, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		After:       p1.Next,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("page 2 failed: %v", err)
	}
	if len(p2.Events) != 2 {
		t.Fatalf("page 2 expected 2 events, got %d", len(p2.Events))
	}
	if p2.Events[0].ID != "msg-3" || p2.Events[1].ID != "msg-4" {
		t.Errorf("page 2 IDs unexpected: %s, %s", p2.Events[0].ID, p2.Events[1].ID)
	}
	if p2.Next != "msg-4" {
		t.Errorf("page 2 Next got %q, want %q", p2.Next, "msg-4")
	}

	// Page 3: after msg-4, limit 2 (last page, only 1 event remains)
	p3, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		After:       p2.Next,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("page 3 failed: %v", err)
	}
	if len(p3.Events) != 1 {
		t.Fatalf("page 3 expected 1 event, got %d", len(p3.Events))
	}
	if p3.Events[0].ID != "msg-5" {
		t.Errorf("page 3 IDs unexpected: %s", p3.Events[0].ID)
	}
	if p3.Next != "" {
		t.Errorf("page 3 Next got %q, want empty string", p3.Next)
	}
}

func TestHistory_UnknownSession(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-unk-s", "ag-unk-s")

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   "nonexistent-session",
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown session: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil HistoryResult")
	}
	if len(res.Events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(res.Events))
	}
	if res.Events == nil {
		t.Fatal("expected non-nil Events slice")
	}
	if res.Next != "" {
		t.Fatalf("expected empty Next, got %q", res.Next)
	}
}

func TestHistory_UnknownCursor(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-unk-c", "ag-unk-c")
	sessionID := "sess-unk-c"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "msg-1", Message: schema.UserAgenticMessage("hello")},
	}
	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		After:       "nonexistent-cursor",
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown cursor: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil HistoryResult")
	}
	if len(res.Events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(res.Events))
	}
	if res.Events == nil {
		t.Fatal("expected non-nil Events slice")
	}
	if res.Next != "" {
		t.Fatalf("expected empty Next, got %q", res.Next)
	}
}

func TestHistory_CrossTenantIsolation(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	wsA, agA := createTestWorkspaceAndAgent(t, ctx, st, "ws-a", "ag-a")
	wsB, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-b", "ag-b")
	sessionID := "sess-shared-id"

	adapterA := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsA.ID)
	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "msg-a1", Message: schema.UserAgenticMessage("tenant A secret")},
	}
	if err := adapterA.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents A failed: %v", err)
	}

	// Query from workspace B without AgentID: should return empty result (events in wsA are invisible)
	resB, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: wsB.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("unexpected error querying from tenant B: %v", err)
	}
	if len(resB.Events) != 0 {
		t.Fatalf("cross-tenant leakage: workspace B saw %d events from workspace A", len(resB.Events))
	}
	if resB.Events == nil {
		t.Fatal("expected non-nil Events slice")
	}
	if resB.Next != "" {
		t.Fatalf("expected empty Next, got %q", resB.Next)
	}

	// Query from workspace B with agent A's ID: should fail with ErrNotFound (agent A not in workspace B)
	_, err = agent.History(ctx, HistoryRequest{
		WorkspaceID: wsB.ID,
		AgentID:     agA.ID,
		SessionID:   sessionID,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected domain.ErrNotFound when querying with foreign agent ID, got %v", err)
	}

	// Query from workspace A: succeeds and sees event
	resA, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: wsA.ID,
		AgentID:     agA.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("workspace A query failed: %v", err)
	}
	if len(resA.Events) != 1 {
		t.Fatalf("expected 1 event for workspace A, got %d", len(resA.Events))
	}
	if resA.Events[0].ID != "msg-a1" {
		t.Errorf("expected event msg-a1, got %s", resA.Events[0].ID)
	}
}
