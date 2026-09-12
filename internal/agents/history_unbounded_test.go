package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Regression suite for fix-session-event-ordering: the stores used to cap
// Limit<=0 reads at 100 rows, so full-log History, PendingApproval scans, and
// compaction replay all silently truncated past event 100. These tests pin
// the unbounded semantics at the Runner level over a >100-event session.

const (
	unboundedEventCount = 130 // > the legacy 100-row cap
	unboundedPerTurn    = 7   // deliberately does not divide the page size below
	unboundedPageSize   = 50
)

// seedUnboundedSession appends n filler events (renderable user messages)
// partitioned into turns of perTurn events each, mirroring how
// history_test.go seeds events through the ADK adapter. Appends happen in
// perTurn-sized batches so cross-call sequence allocation is exercised too.
// Returns the event IDs in append order.
func seedUnboundedSession(t *testing.T, ctx context.Context, adapter *ADKSessionAdapter, sessionID string, n, perTurn int) []string {
	t.Helper()
	base := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	ids := make([]string, 0, n)
	for start := 0; start < n; start += perTurn {
		end := start + perTurn
		if end > n {
			end = n
		}
		var batch []*adk.SessionEvent[*schema.AgenticMessage]
		for i := start; i < end; i++ {
			id := fmt.Sprintf("evt-%04d", i+1)
			ids = append(ids, id)
			batch = append(batch, &adk.SessionEvent[*schema.AgenticMessage]{
				EventID:   id,
				TurnID:    fmt.Sprintf("turn-%03d", i/perTurn+1),
				Timestamp: base.Add(time.Duration(i) * time.Second),
				Message:   schema.UserAgenticMessage(fmt.Sprintf("msg %s", id)),
			})
		}
		if err := adapter.AppendEvents(ctx, sessionID, batch); err != nil {
			t.Fatalf("AppendEvents batch %d..%d: %v", start, end-1, err)
		}
	}
	return ids
}

// historySignature is the per-event identity used to compare a full load
// against a paged walk: kind, event id (empty for synthesized turn
// terminals), and turn id.
func historySignature(e TranscriptEvent) string {
	return fmt.Sprintf("%s|%s|%s", e.Kind, e.ID, e.TurnID)
}

func signatures(events []TranscriptEvent) []string {
	out := make([]string, 0, len(events))
	for i := range events {
		out = append(out, historySignature(events[i]))
	}
	return out
}

func TestHistory_UnboundedFullLog(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-unb", "ag-unb")
	sessionID := "sess-unb-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	ids := seedUnboundedSession(t, ctx, adapter, sessionID, unboundedEventCount, unboundedPerTurn)

	turns := (unboundedEventCount + unboundedPerTurn - 1) / unboundedPerTurn

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("agent.History failed: %v", err)
	}

	// No-limit load reports no next cursor.
	if res.Next != "" {
		t.Fatalf("unbounded load Next = %q, want empty", res.Next)
	}

	// Every persisted message plus one turn_completed per turn.
	wantCount := unboundedEventCount + turns
	if len(res.Events) != wantCount {
		t.Fatalf("expected %d translated events (%d messages + %d terminals), got %d",
			wantCount, unboundedEventCount, turns, len(res.Events))
	}

	// All turns are covered, including those past the old 100-row cap: the
	// message events must be exactly the seeded ids in append order.
	var msgIdx int
	for _, ev := range res.Events {
		if ev.Kind != TranscriptEventMessageCompleted {
			continue
		}
		if msgIdx >= len(ids) {
			t.Fatalf("more message events than seeded ids (at %q)", ev.ID)
		}
		if ev.ID != ids[msgIdx] {
			t.Fatalf("message %d: got id %q, want %q (order broken or loss/duplication)", msgIdx, ev.ID, ids[msgIdx])
		}
		msgIdx++
	}
	if msgIdx != len(ids) {
		t.Fatalf("saw %d message events, want %d", msgIdx, len(ids))
	}

	// One terminal per turn, in turn order.
	var terminals []string
	for _, ev := range res.Events {
		if ev.Kind == TranscriptEventTurnCompleted {
			terminals = append(terminals, ev.TurnID)
		}
	}
	if len(terminals) != turns {
		t.Fatalf("got %d turn_completed events, want %d", len(terminals), turns)
	}
	for i, turnID := range terminals {
		want := fmt.Sprintf("turn-%03d", i+1)
		if turnID != want {
			t.Fatalf("terminal %d: turn id %q, want %q", i, turnID, want)
		}
	}

	// The final turn_completed IS emitted on a full load (the next == ""
	// gate is the terminal emitter when no cursor was requested) — under the
	// old silent 100-row cap the truncated tail corrupted exactly this.
	last := res.Events[len(res.Events)-1]
	if last.Kind != TranscriptEventTurnCompleted || last.TurnID != fmt.Sprintf("turn-%03d", turns) {
		t.Fatalf("last event = %+v, want turn_completed for turn-%03d", last, turns)
	}
}

func TestHistory_UnboundedPaging(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-unb-page", "ag-unb-page")
	sessionID := "sess-unb-page-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedUnboundedSession(t, ctx, adapter, sessionID, unboundedEventCount, unboundedPerTurn)

	full, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("full load failed: %v", err)
	}
	want := signatures(full.Events)

	// Walk the whole session with Limit pages + Next cursors.
	var walked []string
	seen := map[string]int{}
	after := ""
	pages := 0
	for {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			After:       after,
			Limit:       unboundedPageSize,
		})
		if err != nil {
			t.Fatalf("page %d failed: %v", pages+1, err)
		}
		pages++
		for i := range res.Events {
			sig := historySignature(res.Events[i])
			walked = append(walked, sig)
			if res.Events[i].ID != "" {
				seen[res.Events[i].ID]++
				if seen[res.Events[i].ID] > 1 {
					t.Fatalf("event %q delivered twice across pages", res.Events[i].ID)
				}
			}
		}
		if res.Next == "" {
			break
		}
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		after = res.Next
	}

	if pages < 2 {
		t.Fatalf("expected multi-page walk over %d events, got %d page(s)", unboundedEventCount, pages)
	}
	if len(walked) != len(want) {
		t.Fatalf("paged walk yielded %d events, full load %d", len(walked), len(want))
	}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("paged walk diverges at %d: got %q, want %q", i, walked[i], want[i])
		}
	}
}

func TestPendingApproval_BeyondHundredthEvent(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-pend", "ag-pend")
	sessionID := "sess-pend-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	// Seed 130 neutral span events (neither interrupt nor turn activity).
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	var fillers []*adk.SessionEvent[*schema.AgenticMessage]
	for i := 0; i < unboundedEventCount; i++ {
		fillers = append(fillers, &adk.SessionEvent[*schema.AgenticMessage]{
			EventID:   fmt.Sprintf("span-%04d", i+1),
			TurnID:    "turn-pend",
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Kind:      adk.SessionEventSpanModelRequestEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindModel,
				EndedAt: base.Add(time.Duration(i) * time.Second),
				Model:   &adk.ModelSpanMeta{},
			},
		})
	}
	if err := adapter.AppendEvents(ctx, sessionID, fillers); err != nil {
		t.Fatalf("AppendEvents fillers: %v", err)
	}

	// No interrupt yet: nothing pending.
	pending, err := agent.PendingApproval(ctx, ws.ID, sessionID)
	if err != nil {
		t.Fatalf("PendingApproval before interrupt: %v", err)
	}
	if pending != nil {
		t.Fatalf("PendingApproval before interrupt = %+v, want nil", pending)
	}

	// The interrupt sits at event 131 — past the legacy 100-row cutoff. An
	// approval buried there used to be invisible to the scan.
	const interruptID = "agent:test;tool:approval-buried"
	if err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{{
		EventID:   "evt-interrupt-buried",
		TurnID:    "turn-pend",
		Timestamp: base.Add(time.Duration(unboundedEventCount) * time.Second),
		Kind:      adk.SessionEventInterrupt,
		Interrupt: &adk.InterruptEvent{
			Contexts: []*adk.InterruptContext{{InterruptID: interruptID}},
		},
	}}); err != nil {
		t.Fatalf("AppendEvents interrupt: %v", err)
	}

	pending, err = agent.PendingApproval(ctx, ws.ID, sessionID)
	if err != nil {
		t.Fatalf("PendingApproval after interrupt: %v", err)
	}
	if pending == nil {
		t.Fatal("PendingApproval = nil, want the buried approval")
	}
	if pending.InterruptID != interruptID {
		t.Fatalf("InterruptID = %q, want %q", pending.InterruptID, interruptID)
	}

	// A later message event is the resolution marker: the approval clears.
	if err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{{
		EventID:   "evt-resume",
		TurnID:    "turn-pend",
		Timestamp: base.Add(time.Duration(unboundedEventCount+1) * time.Second),
		Message:   schema.UserAgenticMessage("approved"),
	}}); err != nil {
		t.Fatalf("AppendEvents resume: %v", err)
	}

	pending, err = agent.PendingApproval(ctx, ws.ID, sessionID)
	if err != nil {
		t.Fatalf("PendingApproval after resume: %v", err)
	}
	if pending != nil {
		t.Fatalf("PendingApproval after resume = %+v, want nil", pending)
	}
}
