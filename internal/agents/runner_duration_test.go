package agents

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The tool-duration tests (tasks.md 1.1, design D5): the runner stamps each
// terminal tool record with the wall-clock duration observed from the call's
// running announcement to its terminal record — for completed and failed
// calls alike.

// stepClock is a deterministic clock for duration tests: every read returns
// the current step and advances by one step, so the window between a call's
// announcement and its terminal stamp is strictly positive without trusting
// real tool latency. Safe for the turn goroutine's concurrent reads.
type stepClock struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func (c *stepClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.cur
	c.cur = c.cur.Add(c.step)
	return t
}

// TestTurn_ToolDurationStampedOnTerminalRecords drives a turn with one
// failing and one completing tool call and asserts both terminal records
// carry a positive duration measured from their announcements.
func TestTurn_ToolDurationStampedOnTerminalRecords(t *testing.T) {
	clock := &stepClock{cur: time.Now().UTC(), step: 5 * time.Millisecond}
	restore := now
	now = clock.now
	t.Cleanup(func() { now = restore })

	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		switch calls {
		case 1:
			// http_get rejects non-http schemes: a deterministic tool failure.
			return toolCallsReply("call-fail", ToolHTTPGet, `{"url":"not-a-url"}`)
		case 2:
			return toolCallsReply("call-ok", ToolCurrentTime, `{}`)
		default:
			return fakeReply{text: "done", promptTokens: 30, completionTokens: 5}
		}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "try, then tell the time")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 2)

	require.Equal(t, sessions.ToolStatusFailed, tools[0].Tool.Status)
	require.Equal(t, "call-fail", tools[0].Tool.CallID)
	require.Positive(t, tools[0].Tool.DurationMs, "a failed record carries a positive duration")

	require.Equal(t, sessions.ToolStatusCompleted, tools[1].Tool.Status)
	require.Equal(t, "call-ok", tools[1].Tool.CallID)
	require.Positive(t, tools[1].Tool.DurationMs, "a completed record carries a positive duration")
}

// TestTurn_ToolDurationZeroWithoutAnnouncementOnRecord drives the terminal
// record path for a call the running map has no announcement time for —
// a replayed pre-change record's shape (tasks.md 1.1: a resumed run's
// pre-interrupt call, or a record persisted before durations existed). The
// record must carry DurationMs 0, which omitempty marshals away, rather
// than a fabricated latency (design D5).
func TestTurn_ToolDurationZeroWithoutAnnouncementOnRecord(t *testing.T) {
	// The clock advances like in the stamped test above: any fabricated
	// duration from a zero start would stand out against it.
	clock := &stepClock{cur: time.Now().UTC(), step: 5 * time.Millisecond}
	restore := now
	now = clock.now
	t.Cleanup(func() { now = restore })

	msg := &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolResult{
				CallID: "call-lost",
				Name:   ToolCurrentTime,
				Content: []*schema.FunctionToolResultContentBlock{
					{
						Type: schema.FunctionToolResultContentBlockTypeText,
						Text: &schema.UserInputText{Text: "14:00"},
					},
				},
			}),
		},
	}
	mo := &adk.TypedMessageVariant[*schema.AgenticMessage]{
		Message: msg,
	}
	var recorded *sessions.Event
	record := func(ev *sessions.Event, _ bool) error {
		recorded = ev
		return nil
	}

	// started is empty: this run never announced the call.
	err := recordToolResult(mo, record, map[string]string{"call-lost": "{}"}, map[string]time.Time{}, newToolErrorResult())
	require.NoError(t, err)

	require.NotNil(t, recorded)
	require.NotNil(t, recorded.Tool)
	require.Equal(t, sessions.ToolStatusCompleted, recorded.Tool.Status)
	require.Equal(t, "call-lost", recorded.Tool.CallID)
	require.Equal(t, 0, recorded.Tool.DurationMs)

	// omitempty drops the zero: the persisted record marshals no duration.
	data, err := json.Marshal(recorded.Tool)
	require.NoError(t, err)
	require.NotContains(t, string(data), "duration_ms")
}

// TestDurationMsSince_SubMillisecondFloorsToOne pins the floor (design D5):
// a measured call that completed inside a millisecond — the task 10.3 smoke's
// local file writes did, and every write_file/edit_file record persisted
// without duration_ms — still states a 1ms latency, while an unrecorded start
// stays zero and is omitted.
func TestDurationMsSince_SubMillisecondFloorsToOne(t *testing.T) {
	fixed := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return fixed.Add(300 * time.Microsecond) }
	t.Cleanup(func() { now = restore })

	require.Equal(t, 1, durationMsSince(fixed))
	require.Equal(t, 0, durationMsSince(time.Time{}))

	now = func() time.Time { return fixed.Add(7 * time.Millisecond) }
	require.Equal(t, 7, durationMsSince(fixed))
}
