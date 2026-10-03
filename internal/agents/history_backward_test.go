package agents

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Backward-window suite for add-server-authoritative-transcript D2: a
// Reverse read with a Before cursor returns exactly the events immediately
// older than the cursor, oldest-to-newest, and HasMore/Oldest chain pages
// back to the log start. Forward reads must stay byte-identical to the
// pre-change contract — no has_more/oldest keys on the wire.

func TestHistory_BackwardWindow(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-back", "ag-back")
	sessionID := "sess-back-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	ids := seedUnboundedSession(t, ctx, adapter, sessionID, 10, 2) // evt-0001..evt-0010, turn-001..turn-005

	t.Run("cursor+limit returns the N events immediately older, ASC", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			Before:      ids[5], // evt-0006
			Limit:       3,
			Reverse:     true,
		})
		if err != nil {
			t.Fatalf("agent.History failed: %v", err)
		}
		wantIDs := []string{ids[2], ids[3], ids[4]} // evt-0003..evt-0005
		var msgIDs []string
		for _, ev := range res.Events {
			if ev.Kind == TranscriptEventMessageCompleted {
				msgIDs = append(msgIDs, ev.ID)
			}
		}
		if len(msgIDs) != len(wantIDs) {
			t.Fatalf("expected %d message events in the window, got %d (%v)", len(wantIDs), len(msgIDs), msgIDs)
		}
		for i, wantID := range wantIDs {
			if msgIDs[i] != wantID {
				t.Errorf("position %d: got %q, want %q (ASC order broken)", i, msgIDs[i], wantID)
			}
		}
		// Older events remain behind the window: has_more + the next cursor.
		if !res.HasMore {
			t.Errorf("HasMore = false, want true (older events exist)")
		}
		if res.Oldest != ids[2] {
			t.Errorf("Oldest = %q, want %q", res.Oldest, ids[2])
		}
		if res.Next != "" {
			t.Errorf("Next = %q, want empty on backward reads", res.Next)
		}
	})

	t.Run("empty page at log start", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			Before:      ids[0], // the oldest event: nothing older remains
			Limit:       3,
			Reverse:     true,
		})
		if err != nil {
			t.Fatalf("agent.History failed: %v", err)
		}
		if len(res.Events) != 0 {
			t.Fatalf("expected 0 events at log start, got %d", len(res.Events))
		}
		if res.HasMore {
			t.Errorf("HasMore = true at log start, want false")
		}
		if res.Oldest != "" {
			t.Errorf("Oldest = %q at log start, want empty", res.Oldest)
		}
	})

	t.Run("limitless backward read returns the whole log", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			Reverse:     true,
		})
		if err != nil {
			t.Fatalf("agent.History failed: %v", err)
		}
		// Limit <= 0 cannot have more behind it: no truncation, no has_more.
		if res.HasMore {
			t.Errorf("HasMore = true on a limitless read, want false")
		}
		if res.Oldest != ids[0] {
			t.Errorf("Oldest = %q, want %q", res.Oldest, ids[0])
		}
		var msgIdx int
		for _, ev := range res.Events {
			if ev.Kind == TranscriptEventMessageCompleted {
				if ev.ID != ids[msgIdx] {
					t.Fatalf("message %d: got %q, want %q (order broken)", msgIdx, ev.ID, ids[msgIdx])
				}
				msgIdx++
			}
		}
		if msgIdx != len(ids) {
			t.Fatalf("saw %d message events, want %d", msgIdx, len(ids))
		}
	})
}

// TestHistory_BackwardChainWalksToLogStart pages the 12-event log backwards
// three events at a time and requires the prepended concatenation to equal
// the full forward read event-for-event. With turns of three events and a
// page size of three, every page is exactly one turn, so each page closes
// its own trailing turn and the walk composes without loss, duplication, or
// reordering.
func TestHistory_BackwardChainWalksToLogStart(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-back-chain", "ag-back-chain")
	sessionID := "sess-back-chain-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedUnboundedSession(t, ctx, adapter, sessionID, 12, 3)

	full, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("full forward load failed: %v", err)
	}
	want := signatures(full.Events)

	var walked []string
	seen := map[string]int{}
	before := ""
	pages := 0
	for {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			Before:      before,
			Limit:       3,
			Reverse:     true,
		})
		if err != nil {
			t.Fatalf("page %d failed: %v", pages+1, err)
		}
		pages++
		page := make([]string, 0, len(res.Events))
		for i := range res.Events {
			sig := historySignature(res.Events[i])
			page = append(page, sig)
			if res.Events[i].ID != "" {
				seen[res.Events[i].ID]++
				if seen[res.Events[i].ID] > 1 {
					t.Fatalf("event %q delivered twice across pages", res.Events[i].ID)
				}
			}
		}
		// The client prepends each older page; mirror that here.
		walked = append(append([]string{}, page...), walked...)
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		if !res.HasMore {
			break
		}
		if res.Oldest == "" {
			t.Fatal("has_more page without an oldest cursor")
		}
		before = res.Oldest
	}

	if pages != 4 {
		t.Fatalf("expected 4 pages over 12 events at limit 3, got %d", pages)
	}
	if len(walked) != len(want) {
		t.Fatalf("backward walk yielded %d events, full forward read %d", len(walked), len(want))
	}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("backward walk diverges at %d: got %q, want %q", i, walked[i], want[i])
		}
	}
}

// TestHistory_BackwardPageClosesTrailingTurn pins that a backward window
// ending mid-turn still synthesizes its trailing turn_completed: the final
// turn-closing branch runs on every backward read (Next is always empty
// there), exactly as on forward reads.
func TestHistory_BackwardPageClosesTrailingTurn(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-back-turn", "ag-back-turn")
	sessionID := "sess-back-turn-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedUnboundedSession(t, ctx, adapter, sessionID, 10, 5) // turn-001: evt-0001..0005, turn-002: evt-0006..0010

	res, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		Limit:       3,
		Reverse:     true,
	})
	if err != nil {
		t.Fatalf("agent.History failed: %v", err)
	}
	if len(res.Events) != 4 {
		t.Fatalf("expected 3 messages + 1 trailing terminal, got %d events", len(res.Events))
	}
	last := res.Events[len(res.Events)-1]
	if last.Kind != TranscriptEventTurnCompleted || last.TurnID != "turn-002" {
		t.Fatalf("last event = %+v, want turn_completed for turn-002", last)
	}
}

func TestHistory_BackwardUnknownCursor(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-back-unk", "ag-back-unk")
	sessionID := "sess-back-unk-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedUnboundedSession(t, ctx, adapter, sessionID, 1, 1)

	_, err := agent.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		Before:      "nonexistent-cursor",
		Limit:       3,
		Reverse:     true,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid for unknown before cursor, got %v", err)
	}
}

// TestHistory_ForwardReadUnchanged pins the additive contract: forward reads
// leave HasMore/Oldest zero so the marshaled response carries no
// has_more/oldest keys, while Next still reports on limited pages.
func TestHistory_ForwardReadUnchanged(t *testing.T) {
	st, agent, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-fwd-unchanged", "ag-fwd-unchanged")
	sessionID := "sess-fwd-unchanged-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	ids := seedUnboundedSession(t, ctx, adapter, sessionID, 10, 2)

	t.Run("limited forward page", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
			Limit:       2,
		})
		if err != nil {
			t.Fatalf("agent.History failed: %v", err)
		}
		if res.HasMore || res.Oldest != "" {
			t.Fatalf("forward read must not report backward cursors, got has_more=%v oldest=%q", res.HasMore, res.Oldest)
		}
		if res.Next != ids[1] {
			t.Fatalf("limited forward Next = %q, want %q", res.Next, ids[1])
		}
		js, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(js), "has_more") || strings.Contains(string(js), "oldest") {
			t.Fatalf("forward response must stay byte-identical (no has_more/oldest keys), got %s", js)
		}
	})

	t.Run("unbounded forward read", func(t *testing.T) {
		res, err := agent.History(ctx, HistoryRequest{
			WorkspaceID: ws.ID,
			SessionID:   sessionID,
		})
		if err != nil {
			t.Fatalf("agent.History failed: %v", err)
		}
		if res.Next != "" || res.HasMore || res.Oldest != "" {
			t.Fatalf("unbounded forward read reported cursors: next=%q has_more=%v oldest=%q", res.Next, res.HasMore, res.Oldest)
		}
		js, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(js), "has_more") || strings.Contains(string(js), "oldest") {
			t.Fatalf("forward response must stay byte-identical (no has_more/oldest keys), got %s", js)
		}
	})
}
