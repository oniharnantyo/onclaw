package sessions_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestToolActivity_StatusResultJSONRoundTrip covers the wire contract of the
// extended tool activity: status and result survive a JSON encode/decode for
// every state, and legacy payloads persisted before the status field existed
// decode unchanged with an empty (terminal) status.
func TestToolActivity_StatusResultJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   sessions.ToolActivity
	}{
		{
			name: "completed with result",
			in: sessions.ToolActivity{
				CallID: "call-1", Name: "read_file", Arguments: `{"path":"a.txt"}`,
				Status: sessions.ToolStatusCompleted, Result: "file body",
			},
		},
		{
			name: "failed with error result",
			in: sessions.ToolActivity{
				CallID: "call-2", Name: "execute",
				Status: sessions.ToolStatusFailed, Result: "exit status 1", IsError: true,
			},
		},
		{
			name: "running (the live-only form still travels the stream)",
			in:   sessions.ToolActivity{CallID: "call-3", Name: "grep", Status: sessions.ToolStatusRunning},
		},
		{
			name: "legacy record without a status",
			in:   sessions.ToolActivity{CallID: "call-4", Name: "lookup", Arguments: `{"q":"x"}`, Result: "found"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(sessions.Event{Kind: sessions.KindToolActivity, Tool: &tc.in})
			require.NoError(t, err)

			var got sessions.Event
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, sessions.KindToolActivity, got.Kind)
			require.NotNil(t, got.Tool)
			require.Equal(t, tc.in, *got.Tool)
		})
	}

	// A pre-status transcript payload (no status key, result already set by
	// the base runtime's callers) decodes into the extended shape unchanged.
	var legacy sessions.ToolActivity
	require.NoError(t, json.Unmarshal(
		[]byte(`{"call_id":"old-1","name":"lookup","result":"found"}`), &legacy))
	require.Equal(t, sessions.ToolActivity{CallID: "old-1", Name: "lookup", Result: "found"}, legacy)
	require.True(t, legacy.Terminal(), "legacy records are terminal")
}

// TestService_ToolTerminalRecordsReplay covers D10's persistence rule at the
// service level: terminal tool records (completed/failed with a real result)
// append and replay with status and result intact, legacy records replay
// unchanged, and the log stays the single durable form.
func TestService_ToolTerminalRecordsReplay(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "go"},
		{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "call-1", Name: "read_file", Arguments: `{"path":"a.txt"}`,
			Status: sessions.ToolStatusCompleted, Result: "file body",
		}},
		{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "call-2", Name: "execute",
			Status: sessions.ToolStatusFailed, Result: "exit status 1", IsError: true,
		}},
		// A pre-status record (what old transcripts hold) must keep
		// replaying byte-for-byte.
		{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "old-1", Name: "lookup", Arguments: `{"q":"x"}`, Result: "found",
		}},
	}))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 4)

	tool := events[1].Tool
	require.NotNil(t, tool)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
	require.Equal(t, "file body", tool.Result)
	require.True(t, tool.Terminal())

	tool = events[2].Tool
	require.NotNil(t, tool)
	require.Equal(t, sessions.ToolStatusFailed, tool.Status)
	require.Equal(t, "exit status 1", tool.Result)
	require.True(t, tool.IsError)

	tool = events[3].Tool
	require.NotNil(t, tool)
	require.Zero(t, tool.Status, "legacy records carry no status")
	require.Equal(t, "found", tool.Result)
}

// TestService_AppendRejectsRunningToolActivity pins the live-only rule of the
// running state (design D10): Append refuses to persist a running tool
// record, and nothing from the rejected batch lands in the transcript.
func TestService_AppendRejectsRunningToolActivity(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	err = svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "go"},
		{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "call-1", Name: "execute", Status: sessions.ToolStatusRunning,
		}},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "still running")

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, events, "the rejected batch must persist nothing")

	// The terminal record of the same call is accepted afterwards.
	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "call-1", Name: "execute",
			Status: sessions.ToolStatusCompleted, Result: "done",
		}},
	}))
	events, err = svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, sessions.ToolStatusCompleted, events[0].Tool.Status)
}

// TestService_EventsSkipsUnknownKinds pins the forward-compatibility rule:
// kinds written by a newer version are skipped on replay without failing the
// transcript, and the known events around them come back in order.
func TestService_EventsSkipsUnknownKinds(t *testing.T) {
	ctx := context.Background()
	svc, fs := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "go"},
	}))

	// Simulate a newer version writing kinds this build does not know.
	require.NoError(t, fs.SessionEvents().AppendEvents(ctx, sess.ID, []store.SessionEvent{
		{EventID: "future-1", Kind: "task_progress", Payload: []byte(`{"task_id":"t1","state":"running"}`)},
	}))

	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindAssistantMessage, Text: "done"},
	}))
	require.NoError(t, fs.SessionEvents().AppendEvents(ctx, sess.ID, []store.SessionEvent{
		{EventID: "future-2", Kind: "something_even_newer", Payload: []byte(`{}`)},
	}))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err, "unknown kinds must be skipped, not error")
	require.Len(t, events, 2)
	require.Equal(t, sessions.KindUserMessage, events[0].Kind)
	require.Equal(t, int64(1), events[0].Seq)
	require.Equal(t, sessions.KindAssistantMessage, events[1].Kind)
	require.Equal(t, int64(3), events[1].Seq, "known events keep their log positions")
}

// TestTaskActivity_JSONRoundTrip covers the task-activity wire contract: the
// task identity, description, status, result, transcript path, and lane all
// survive a JSON encode/decode for every lifecycle state, and a payload
// written without the newer fields decodes into the same shape (adding a
// field must never break a replay).
func TestTaskActivity_JSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   sessions.TaskActivity
	}{
		{
			name: "completed background task",
			in: sessions.TaskActivity{
				TaskID: "task-1", Description: "summarize the repo",
				Status: sessions.ToolStatusCompleted, Result: "the repo does X",
				TranscriptPath: ".tasks/task-1.md", Background: true,
			},
		},
		{
			name: "failed foreground task",
			in: sessions.TaskActivity{
				TaskID: "task-2", Description: "read the log",
				Status: sessions.ToolStatusFailed, Result: "model call failed",
			},
		},
		{
			name: "stopped task",
			in: sessions.TaskActivity{
				TaskID: "task-3", Description: "long search",
				Status: sessions.ToolStatusStopped,
			},
		},
		{
			name: "running (the live-only form still travels the stream)",
			in: sessions.TaskActivity{
				TaskID: "task-4", Description: "still going",
				Status: sessions.ToolStatusRunning, Background: true,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(sessions.Event{Kind: sessions.KindTaskActivity, Task: &tc.in})
			require.NoError(t, err)

			var got sessions.Event
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, sessions.KindTaskActivity, got.Kind)
			require.NotNil(t, got.Task)
			require.Equal(t, tc.in, *got.Task)
		})
	}

	// A minimal payload decodes into the zero-valued extension fields, so a
	// transcript written before a field existed keeps replaying.
	var minimal sessions.TaskActivity
	require.NoError(t, json.Unmarshal([]byte(`{"task_id":"old-1","status":"completed","result":"done"}`), &minimal))
	require.Equal(t, sessions.TaskActivity{
		TaskID: "old-1", Status: sessions.ToolStatusCompleted, Result: "done",
	}, minimal)
	require.True(t, minimal.Terminal())
}

// TestService_TaskTerminalRecordsReplay covers D10's persistence rule for
// delegated tasks: terminal records (completed/failed/stopped) append and
// replay with status, result, and transcript path intact, and a running
// record is refused.
func TestService_TaskTerminalRecordsReplay(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "delegate"},
		{Kind: sessions.KindTaskActivity, Task: &sessions.TaskActivity{
			TaskID: "task-1", Description: "summarize", Status: sessions.ToolStatusCompleted,
			Result: "the summary", TranscriptPath: ".tasks/task-1.md", Background: true,
		}},
		{Kind: sessions.KindTaskActivity, Task: &sessions.TaskActivity{
			TaskID: "task-2", Description: "read the log", Status: sessions.ToolStatusFailed,
			Result: "child run failed",
		}},
		{Kind: sessions.KindTaskActivity, Task: &sessions.TaskActivity{
			TaskID: "task-3", Description: "long search", Status: sessions.ToolStatusStopped,
		}},
	}))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 4)

	completed := events[1].Task
	require.NotNil(t, completed)
	require.Equal(t, sessions.ToolStatusCompleted, completed.Status)
	require.Equal(t, "the summary", completed.Result)
	require.Equal(t, ".tasks/task-1.md", completed.TranscriptPath)
	require.True(t, completed.Background)

	require.Equal(t, sessions.ToolStatusFailed, events[2].Task.Status)
	require.Equal(t, "child run failed", events[2].Task.Result)
	require.False(t, events[2].Task.Background)

	require.Equal(t, sessions.ToolStatusStopped, events[3].Task.Status)
	require.True(t, events[3].Task.Terminal())
}

// TestService_AppendRejectsRunningTaskActivity pins the live-only rule for
// delegated tasks (design D10): Append refuses to persist a running task
// record, and nothing from the rejected batch lands in the transcript.
func TestService_AppendRejectsRunningTaskActivity(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	err = svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "go"},
		{Kind: sessions.KindTaskActivity, Task: &sessions.TaskActivity{
			TaskID: "task-1", Description: "long search", Status: sessions.ToolStatusRunning,
		}},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "still running")

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, events, "the rejected batch must persist nothing")

	// The terminal record of the same task is accepted afterwards.
	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindTaskActivity, Task: &sessions.TaskActivity{
			TaskID: "task-1", Description: "long search",
			Status: sessions.ToolStatusCompleted, Result: "found it",
		}},
	}))
	events, err = svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, sessions.ToolStatusCompleted, events[0].Task.Status)
}
