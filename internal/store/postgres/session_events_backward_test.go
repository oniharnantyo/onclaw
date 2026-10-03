//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// Backward-keyset suite for add-server-authoritative-transcript D2: the
// store's Reverse path returns the window ending at the cursor newest-first,
// and HasMore/Oldest chaining in History relies on exactly that shape. The
// fake store's mirror of this contract lives in
// internal/store/fake/session_events_ordering_test.go; this pins it on real
// Postgres.

// TestIntegration_SessionEventsBackwardPagination verifies a Reverse+cursor
// load returns at most Limit events strictly older than the cursor event,
// newest-first, and that a chained walk exhausts the log in the exact mirror
// of the forward read.
func TestIntegration_SessionEventsBackwardPagination(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{
		Slug: fmt.Sprintf("ws-back-%s", uuid.NewString()[:8]),
		Name: "Backward WS",
	}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	const sessionID = "sess-backward-1"
	const n = 10
	se := s.SessionEvents()

	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	events := make([]domain.SessionEvent, 0, n)
	for i := 0; i < n; i++ {
		events = append(events, domain.SessionEvent{
			SessionID:  sessionID,
			EventID:    fmt.Sprintf("evt-%03d", i),
			TurnID:     fmt.Sprintf("turn-%d", i/3),
			Seq:        int64(i),
			Kind:       "message",
			Payload:    []byte(fmt.Sprintf(`{"i":%d}`, i)),
			OccurredAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if err := se.AppendEvents(ctx, ws.ID, events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	forward, err := se.LoadEvents(ctx, storeport.LoadSessionEventsParams{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("forward load: %v", err)
	}
	if len(forward) != n {
		t.Fatalf("forward load returned %d events, want %d", len(forward), n)
	}

	t.Run("reverse window with cursor and limit", func(t *testing.T) {
		// The events immediately older than evt-006, at most 3, newest-first
		// (DESC is the store's contract; History reverses to ASC).
		got, err := se.LoadEvents(ctx, storeport.LoadSessionEventsParams{
			WorkspaceID:  ws.ID,
			SessionID:    sessionID,
			AfterEventID: "evt-006",
			Limit:        3,
			Reverse:      true,
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		want := []string{"evt-005", "evt-004", "evt-003"}
		if len(got) != len(want) {
			t.Fatalf("window returned %d events, want %d", len(got), len(want))
		}
		for i, wantID := range want {
			if got[i].EventID != wantID {
				t.Errorf("position %d: got %q, want %q (DESC window broken)", i, got[i].EventID, wantID)
			}
		}
	})

	t.Run("window at log start is empty", func(t *testing.T) {
		got, err := se.LoadEvents(ctx, storeport.LoadSessionEventsParams{
			WorkspaceID:  ws.ID,
			SessionID:    sessionID,
			AfterEventID: "evt-000",
			Limit:        3,
			Reverse:      true,
		})
		if err != nil {
			t.Fatalf("LoadEvents: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected an empty window at log start, got %d events", len(got))
		}
	})

	t.Run("chained walk mirrors the forward read", func(t *testing.T) {
		var walked []domain.SessionEvent
		before := ""
		pages := 0
		for {
			got, err := se.LoadEvents(ctx, storeport.LoadSessionEventsParams{
				WorkspaceID:  ws.ID,
				SessionID:    sessionID,
				AfterEventID: before,
				Limit:        3,
				Reverse:      true,
			})
			if err != nil {
				t.Fatalf("page %d: %v", pages+1, err)
			}
			pages++
			// The DESC page's last row is the oldest returned event — the
			// next backward cursor. Capture it before the in-place reversal.
			oldest := got[len(got)-1].EventID
			// The store delivers each page newest-first; History reverses it
			// to ASC before the client prepends. Mirror that here.
			for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 {
				got[i], got[j] = got[j], got[i]
			}
			// The client prepends each older page; mirror that here.
			walked = append(append([]domain.SessionEvent{}, got...), walked...)
			if len(got) < 3 {
				break
			}
			before = oldest
			if pages > 10 {
				t.Fatal("pagination did not terminate")
			}
		}
		if pages != 4 {
			t.Fatalf("expected 4 pages over %d events at limit 3, got %d", n, pages)
		}
		if len(walked) != len(forward) {
			t.Fatalf("walked %d events, forward read %d", len(walked), len(forward))
		}
		for i := range forward {
			if walked[i].EventID != forward[i].EventID {
				t.Fatalf("walk diverges at %d: got %q, want %q", i, walked[i].EventID, forward[i].EventID)
			}
		}
	})

	t.Run("unknown cursor is not found", func(t *testing.T) {
		// The contract agents.History maps to the endpoint's invalid-request
		// error (add-server-authoritative-transcript D2): the store reports
		// domain.ErrNotFound for a cursor the session does not contain.
		_, err := se.LoadEvents(ctx, storeport.LoadSessionEventsParams{
			WorkspaceID:  ws.ID,
			SessionID:    sessionID,
			AfterEventID: "evt-nope",
			Limit:        3,
			Reverse:      true,
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want domain.ErrNotFound for unknown cursor, got %v", err)
		}
	})
}
