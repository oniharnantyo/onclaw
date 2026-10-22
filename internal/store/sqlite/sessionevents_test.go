package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestSessionEvents_AppendReplayTranscript covers task 3.4's replay
// requirement: a written transcript is reconstructed exactly — same events,
// same order (by store-assigned Seq), payloads intact — across separate
// reads and store instances.
func TestSessionEvents_AppendReplayTranscript(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/events.db"

	first, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, first.SessionEvents().AppendEvents(ctx, "sess-1", eventBatch("e1", "e2")))
	require.NoError(t, first.SessionEvents().AppendEvents(ctx, "sess-1", eventBatch("e3")))
	require.NoError(t, first.Close())

	// "Restart": a brand-new store over the same database replays the
	// transcript in full.
	second, err := Open(ctx, path)
	require.NoError(t, err)
	defer second.Close()

	events, err := second.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, events, 3)
	for i, ev := range events {
		require.Equal(t, int64(i+1), ev.Seq, "sequences must be 1..n in append order")
		require.Equal(t, "sess-1", ev.SessionID)
		require.Equal(t, "turn-1", ev.TurnID)
		id := "e" + fmt.Sprint(i+1)
		require.Equal(t, []byte(`{"text":"`+id+`"}`), ev.Payload, "payloads must round-trip exactly")
		require.False(t, ev.OccurredAt.IsZero())
	}
}

// TestSessionEvents_MonotonicSeqAcrossBatches asserts the per-session
// monotonic sequence guarantee across many appends, and that a retried
// EventID neither duplicates nor reorders the log.
func TestSessionEvents_MonotonicSeqAcrossBatches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for batch := 0; batch < 5; batch++ {
		require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1",
			eventBatch("e"+string(rune('a'+batch))+"-1", "e"+string(rune('a'+batch))+"-2")))
	}
	// Retry of an old event plus a new one.
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", eventBatch("ea-1", "e-new")))

	events, err := s.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, events, 11, "the retried event must not duplicate")

	last := int64(0)
	for _, ev := range events {
		require.Equal(t, last+1, ev.Seq, "sequences must be contiguous and increasing per session")
		last = ev.Seq
	}
}

// TestSessionEvents_SessionsAreIsolated covers session-scoped reads: another
// session's events never leak, and unknown sessions read back empty.
func TestSessionEvents_SessionsAreIsolated(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-a", eventBatch("shared-id")))
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-b", eventBatch("shared-id")))

	a, err := s.SessionEvents().LoadEvents(ctx, "sess-a")
	require.NoError(t, err)
	require.Len(t, a, 1)
	require.Equal(t, int64(1), a[0].Seq, "identical event ids in different sessions get independent sequences")

	b, err := s.SessionEvents().LoadEvents(ctx, "sess-b")
	require.NoError(t, err)
	require.Len(t, b, 1)

	none, err := s.SessionEvents().LoadEvents(ctx, "sess-unknown")
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// TestSessionEvents_EmptyBatchAndEmptySessionID covers the edge inputs: an
// empty batch is a no-op and an empty session id is rejected.
func TestSessionEvents_EmptyBatchAndEmptySessionID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", nil))
	require.Error(t, s.SessionEvents().AppendEvents(ctx, "", eventBatch("e1")))
}

// TestSessionEvents_ToolTerminalRecordRoundTrip covers task 1.2's persistence
// requirement (design D10): a terminal tool record — status completed/failed
// with its real result in the JSON payload — survives the database byte for
// byte, and replaying the stored transcript from a reopened store yields the
// same events. The payload is opaque JSON here, exactly as the store treats
// it; the sessions layer owns its shape.
func TestSessionEvents_ToolTerminalRecordRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/tools.db"

	completed := store.SessionEvent{
		EventID: "tool-1", TurnID: "turn-1", Kind: "tool_activity",
		Payload: []byte(`{"call_id":"call-1","name":"read_file","arguments":"{\"path\":\"a.txt\"}","status":"completed","result":"file body"}`),
	}
	failed := store.SessionEvent{
		EventID: "tool-2", TurnID: "turn-1", Kind: "tool_activity",
		Payload: []byte(`{"call_id":"call-2","name":"execute","status":"failed","result":"exit status 1","is_error":true}`),
	}

	first, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, first.SessionEvents().AppendEvents(ctx, "sess-1", []store.SessionEvent{completed, failed}))
	// In-store replay before restart: the written rows read back as written.
	live, err := first.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, live, 2)
	require.NoError(t, first.Close())

	second, err := Open(ctx, path)
	require.NoError(t, err)
	defer second.Close()

	replayed, err := second.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, replayed, len(live), "replay must yield the same transcript")
	for i := range replayed {
		require.Equal(t, live[i].EventID, replayed[i].EventID)
		require.Equal(t, live[i].Kind, replayed[i].Kind)
		require.JSONEq(t, string(live[i].Payload), string(replayed[i].Payload))
	}

	// Spot-check the terminal fields the sessions layer decodes.
	var done struct {
		CallID    string `json:"call_id"`
		Status    string `json:"status"`
		Result    string `json:"result"`
		Arguments string `json:"arguments"`
		IsError   bool   `json:"is_error"`
	}
	require.NoError(t, json.Unmarshal(replayed[0].Payload, &done))
	require.Equal(t, "call-1", done.CallID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, "file body", done.Result)
	require.Equal(t, `{"path":"a.txt"}`, done.Arguments)
	require.False(t, done.IsError)

	require.NoError(t, json.Unmarshal(replayed[1].Payload, &done))
	require.Equal(t, "call-2", done.CallID)
	require.Equal(t, "failed", done.Status)
	require.Equal(t, "exit status 1", done.Result)
	require.True(t, done.IsError)
}

// TestSessionEvents_TaskActivityRoundTrip covers task 5.5's persistence
// requirement for the delegated-task kind: the running record's terminal
// successor — status, result, transcript path, and lane in the opaque JSON
// payload — survives the database byte for byte across a reopened store, and
// the sessions layer's decode of the replayed payload yields the same task.
func TestSessionEvents_TaskActivityRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/tasks.db"

	completed := store.SessionEvent{
		EventID: "task-1", TurnID: "turn-1", Kind: "task_activity",
		Payload: []byte(`{"task_id":"task-1","description":"summarize the repo","status":"completed","result":"the repo does X","transcript_path":".tasks/task-1.md","background":true}`),
	}
	stopped := store.SessionEvent{
		EventID: "task-2", TurnID: "turn-1", Kind: "task_activity",
		Payload: []byte(`{"task_id":"task-2","description":"long search","status":"stopped"}`),
	}

	first, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, first.SessionEvents().AppendEvents(ctx, "sess-1", []store.SessionEvent{completed, stopped}))
	live, err := first.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, live, 2)
	require.NoError(t, first.Close())

	second, err := Open(ctx, path)
	require.NoError(t, err)
	defer second.Close()

	replayed, err := second.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, replayed, len(live), "replay must yield the same transcript")
	for i := range replayed {
		require.Equal(t, live[i].EventID, replayed[i].EventID)
		require.Equal(t, "task_activity", replayed[i].Kind)
		require.JSONEq(t, string(live[i].Payload), string(replayed[i].Payload))
	}

	// Spot-check the task fields the sessions layer decodes.
	var task struct {
		TaskID         string `json:"task_id"`
		Description    string `json:"description"`
		Status         string `json:"status"`
		Result         string `json:"result"`
		TranscriptPath string `json:"transcript_path"`
		Background     bool   `json:"background"`
	}
	require.NoError(t, json.Unmarshal(replayed[0].Payload, &task))
	require.Equal(t, "task-1", task.TaskID)
	require.Equal(t, "completed", task.Status)
	require.Equal(t, "the repo does X", task.Result)
	require.Equal(t, ".tasks/task-1.md", task.TranscriptPath)
	require.True(t, task.Background)

	// A fresh decode: the stopped payload carries none of the completion
	// fields, so the absent keys stay zero-valued.
	task = struct {
		TaskID         string `json:"task_id"`
		Description    string `json:"description"`
		Status         string `json:"status"`
		Result         string `json:"result"`
		TranscriptPath string `json:"transcript_path"`
		Background     bool   `json:"background"`
	}{}
	require.NoError(t, json.Unmarshal(replayed[1].Payload, &task))
	require.Equal(t, "task-2", task.TaskID)
	require.Equal(t, "stopped", task.Status)
	require.Empty(t, task.Result)
	require.False(t, task.Background)
}
