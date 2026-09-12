package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// seqTestEvent builds a minimal valid ADK session event.
func seqTestEvent(eventID string, at time.Time) *adk.SessionEvent[*schema.AgenticMessage] {
	return &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   eventID,
		TurnID:    "turn-seq",
		Timestamp: at,
		Message:   schema.UserAgenticMessage(fmt.Sprintf("msg %s", eventID)),
	}
}

// TestAppendEvents_SeqMonotonicPastHundred is the fix-session-event-ordering
// regression (task 2.5): the old AppendEvents allocated nextSeq =
// len(existing) from a load the stores silently capped at 100 rows, so every
// event past the 100th collapsed onto the same seq and loads returned
// arbitrary unordered subsets. With NextEventSeq allocation the stored seqs
// stay strictly increasing and append-ordered regardless of session length,
// and a later append into the same session continues the sequence.
func TestAppendEvents_SeqMonotonicPastHundred(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	const wsID = "ws-seq"
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsID)
	sessionID := "sess-seq-long"

	base := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	// Drive >100 events through AppendEvents across batches (as turns do).
	const firstBatches = 13
	const perBatch = 10
	appended := 0
	for b := 0; b < firstBatches; b++ {
		batch := make([]*adk.SessionEvent[*schema.AgenticMessage], 0, perBatch)
		for i := 0; i < perBatch; i++ {
			batch = append(batch, seqTestEvent(fmt.Sprintf("evt-%04d", appended), base.Add(time.Duration(appended)*time.Second)))
			appended++
		}
		if err := adapter.AppendEvents(ctx, sessionID, batch); err != nil {
			t.Fatalf("AppendEvents batch %d: %v", b, err)
		}
	}

	// A second append into the same session must continue the sequence.
	continuation := []*adk.SessionEvent[*schema.AgenticMessage]{
		seqTestEvent("evt-continuation-1", base.Add(time.Duration(appended)*time.Second)),
		seqTestEvent("evt-continuation-2", base.Add(time.Duration(appended+1)*time.Second)),
	}
	if err := adapter.AppendEvents(ctx, sessionID, continuation); err != nil {
		t.Fatalf("AppendEvents continuation: %v", err)
	}
	appended += len(continuation)

	rows, err := st.SessionEvents().LoadEvents(ctx, storeLoadAllParams(wsID, sessionID))
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(rows) != appended {
		t.Fatalf("stored %d rows, want %d", len(rows), appended)
	}

	wantEventID := func(i int) string {
		if i < appended-len(continuation) {
			return fmt.Sprintf("evt-%04d", i)
		}
		return fmt.Sprintf("evt-continuation-%d", i-(appended-len(continuation))+1)
	}

	prev := int64(-1)
	for i, row := range rows {
		if row.Seq != prev+1 {
			t.Fatalf("row %d (%s): seq %d is not prev+1 (prev %d) — allocation collapsed or reordered", i, row.EventID, row.Seq, prev)
		}
		if want := wantEventID(i); row.EventID != want {
			t.Fatalf("row %d: event %q out of append order, want %q", i, row.EventID, want)
		}
		prev = row.Seq
	}
	if rows[0].Seq != 0 {
		t.Fatalf("first seq is %d, want 0", rows[0].Seq)
	}

	// An empty session's next seq starts at 0 (first batch landed at 0 above);
	// after the appends, the next position continues past the last row.
	next, err := st.SessionEvents().NextEventSeq(ctx, wsID, sessionID)
	if err != nil {
		t.Fatalf("NextEventSeq: %v", err)
	}
	if next != int64(appended) {
		t.Fatalf("NextEventSeq after %d appends is %d, want %d", appended, next, appended)
	}
}

// storeLoadAllParams builds an unbounded (Limit <= 0 = no limit) load request.
func storeLoadAllParams(wsID, sessionID string) store.LoadSessionEventsParams {
	return store.LoadSessionEventsParams{WorkspaceID: wsID, SessionID: sessionID}
}

// TestAppendEvents_CrossCallDuplicateEventID verifies an event ID already
// stored in a previous AppendEvents call is rejected with
// adk.ErrDuplicateEventID (existence probe path, no full pre-read).
func TestAppendEvents_CrossCallDuplicateEventID(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	const wsID = "ws-dup"
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsID)
	sessionID := "sess-dup"

	first := seqTestEvent("evt-dup-1", time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC))
	if err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{first}); err != nil {
		t.Fatalf("first AppendEvents: %v", err)
	}

	again := seqTestEvent("evt-dup-1", time.Date(2026, 9, 11, 8, 0, 1, 0, time.UTC))
	err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{again})
	if !errors.Is(err, adk.ErrDuplicateEventID) {
		t.Fatalf("cross-call duplicate: got %v, want adk.ErrDuplicateEventID", err)
	}
}

// TestAppendEvents_WithinBatchDuplicateEventID verifies duplicate IDs inside a
// single batch are still rejected with adk.ErrDuplicateEventID.
func TestAppendEvents_WithinBatchDuplicateEventID(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	const wsID = "ws-batchdup"
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsID)

	at := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	err := adapter.AppendEvents(ctx, "sess-batchdup", []*adk.SessionEvent[*schema.AgenticMessage]{
		seqTestEvent("evt-same", at),
		seqTestEvent("evt-same", at.Add(time.Second)),
	})
	if !errors.Is(err, adk.ErrDuplicateEventID) {
		t.Fatalf("within-batch duplicate: got %v, want adk.ErrDuplicateEventID", err)
	}
}

// TestAppendEvents_EmptyEventID verifies empty event IDs are rejected with
// adk.ErrInvalidEventID.
func TestAppendEvents_EmptyEventID(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	const wsID = "ws-emptyid"
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), wsID)

	err := adapter.AppendEvents(ctx, "sess-emptyid", []*adk.SessionEvent[*schema.AgenticMessage]{
		seqTestEvent("", time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)),
	})
	if !errors.Is(err, adk.ErrInvalidEventID) {
		t.Fatalf("empty event ID: got %v, want adk.ErrInvalidEventID", err)
	}
}
