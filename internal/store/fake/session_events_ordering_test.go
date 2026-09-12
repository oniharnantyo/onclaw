package fake_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// buildOrderedEvents builds n events with strictly increasing seq (0..n-1),
// distinct event ids, and increasing occurred_at, alternating between two
// kinds ("message" for kindMod multiples of kindEvery == 0, "tool" otherwise —
// see the caller for the exact split).
func buildOrderedEvents(sessionID string, n int) []domain.SessionEvent {
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	events := make([]domain.SessionEvent, 0, n)
	for i := 0; i < n; i++ {
		kind := "message"
		if i%3 == 0 {
			kind = "tool"
		}
		events = append(events, domain.SessionEvent{
			SessionID:  sessionID,
			EventID:    fmt.Sprintf("evt-%03d", i),
			TurnID:     fmt.Sprintf("turn-%d", i/2),
			Seq:        int64(i),
			Kind:       kind,
			Payload:    []byte(fmt.Sprintf(`{"i":%d}`, i)),
			OccurredAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	return events
}

// TestSessionEventStore_UnboundedLoadPastHundred verifies the port contract
// fix (fix-session-event-ordering D1): Limit <= 0 means "no limit" — a session
// with more than 100 events loads completely, pages correctly under an
// explicit positive limit, reverses to the exact mirror, and kind-filtered
// unbounded loads return every match regardless of log length.
func TestSessionEventStore_UnboundedLoadPastHundred(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	se := s.SessionEvents()

	const n = 130
	const wsID = "ws-ordering"
	const sessionID = "sess-ordering"

	events := buildOrderedEvents(sessionID, n)
	if err := se.AppendEvents(ctx, wsID, events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	t.Run("unbounded load returns all events in append order", func(t *testing.T) {
		got, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
			Limit:       0, // no limit
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		if len(got) != n {
			t.Fatalf("unbounded load returned %d events, want %d (the old 100-row default cap)", len(got), n)
		}
		for i, e := range got {
			if e.EventID != fmt.Sprintf("evt-%03d", i) || e.Seq != int64(i) {
				t.Fatalf("position %d: got event %q seq %d, want evt-%03d seq %d", i, e.EventID, e.Seq, i, i)
			}
		}
	})

	t.Run("negative limit is also no limit", func(t *testing.T) {
		got, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
			Limit:       -1,
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		if len(got) != n {
			t.Fatalf("Limit=-1 returned %d events, want %d", len(got), n)
		}
	})

	t.Run("explicit limit pages correctly", func(t *testing.T) {
		const page = 50
		cursor := ""
		seen := 0
		for {
			params := store.LoadSessionEventsParams{
				WorkspaceID: wsID,
				SessionID:   sessionID,
				Limit:       page,
			}
			if cursor != "" {
				params.AfterEventID = cursor
			}
			got, err := se.LoadEvents(ctx, params)
			if err != nil {
				t.Fatalf("LoadEvents page at cursor %q: %v", cursor, err)
			}
			for i, e := range got {
				want := seen + i
				if want >= n {
					t.Fatalf("page at cursor %q returned more events than exist (index %d)", cursor, want)
				}
				if e.EventID != fmt.Sprintf("evt-%03d", want) {
					t.Fatalf("page at cursor %q position %d: got %q, want evt-%03d", cursor, i, e.EventID, want)
				}
			}
			seen += len(got)
			if len(got) < page {
				break
			}
			cursor = got[len(got)-1].EventID
		}
		if seen != n {
			t.Fatalf("pagination collected %d events, want %d", seen, n)
		}
	})

	t.Run("reverse is the exact mirror of forward order", func(t *testing.T) {
		fwd, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
		})
		if err != nil {
			t.Fatalf("forward LoadEvents: %v", err)
		}
		rev, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
			Reverse:     true,
		})
		if err != nil {
			t.Fatalf("reverse LoadEvents: %v", err)
		}
		if len(rev) != len(fwd) {
			t.Fatalf("reverse returned %d events, forward returned %d", len(rev), len(fwd))
		}
		for i := range fwd {
			if rev[len(rev)-1-i].EventID != fwd[i].EventID {
				t.Fatalf("mirror mismatch at forward position %d: reverse[%d] is %q, want %q",
					i, len(rev)-1-i, rev[len(rev)-1-i].EventID, fwd[i].EventID)
			}
		}
	})

	t.Run("kind filter with unbounded load returns all matching kinds", func(t *testing.T) {
		// buildOrderedEvents marks every third index "tool"; the remaining
		// "message" rows number well over the old 100-row default, so a cap
		// would truncate them.
		got, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
			Kinds:       []string{"message"},
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		wantCount := 0
		for i := 0; i < n; i++ {
			if i%3 != 0 {
				wantCount++
			}
		}
		if len(got) != wantCount {
			t.Fatalf("kind-filtered unbounded load returned %d events, want %d", len(got), wantCount)
		}
		for _, e := range got {
			if e.Kind != "message" {
				t.Fatalf("kind filter leaked event %q of kind %q", e.EventID, e.Kind)
			}
		}
	})
}

// TestSessionEventStore_TiedSeqDeterministicOrder is the D2 seatbelt: rows
// carrying duplicate seq values (legacy corrupted logs) are returned in
// deterministic (occurred_at, event_id) order, and two consecutive loads
// return identical order.
func TestSessionEventStore_TiedSeqDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	se := s.SessionEvents()

	const wsID = "ws-tied"
	const sessionID = "sess-tied"

	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	// Inserted scrambled on purpose: all tied on seq, the last pair also tied
	// on occurred_at so only the event_id breaks that tie.
	tied := []domain.SessionEvent{
		{SessionID: sessionID, EventID: "evt-c", TurnID: "t", Seq: 42, Kind: "message", OccurredAt: base.Add(2 * time.Second)},
		{SessionID: sessionID, EventID: "evt-a", TurnID: "t", Seq: 42, Kind: "message", OccurredAt: base},
		{SessionID: sessionID, EventID: "evt-b", TurnID: "t", Seq: 42, Kind: "message", OccurredAt: base.Add(1 * time.Second)},
		{SessionID: sessionID, EventID: "evt-d", TurnID: "t", Seq: 42, Kind: "message", OccurredAt: base.Add(2 * time.Second)},
	}
	// Reverse before insert so the stored slice order disagrees with the
	// expected load order.
	for i, j := 0, len(tied)-1; i < j; i, j = i+1, j-1 {
		tied[i], tied[j] = tied[j], tied[i]
	}
	if err := se.AppendEvents(ctx, wsID, tied); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	load := func() []string {
		t.Helper()
		got, err := se.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: wsID,
			SessionID:   sessionID,
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		ids := make([]string, 0, len(got))
		for _, e := range got {
			ids = append(ids, e.EventID)
		}
		return ids
	}

	want := []string{"evt-a", "evt-b", "evt-c", "evt-d"}
	first := load()
	if len(first) != len(want) {
		t.Fatalf("tied-seq load returned %d events, want %d", len(first), len(want))
	}
	for i := range want {
		if first[i] != want[i] {
			t.Fatalf("tied-seq order: got %v, want %v (occurred_at, then event_id must break seq ties)", first, want)
		}
	}
	second := load()
	for i := range first {
		if second[i] != first[i] {
			t.Fatalf("two consecutive loads disagree: first %v, second %v", first, second)
		}
	}
}
