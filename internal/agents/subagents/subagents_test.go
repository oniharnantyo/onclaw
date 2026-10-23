package subagents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// readTranscriptEntries decodes the JSONL transcript at base/rel.
func readTranscriptEntries(t *testing.T, base, rel string) []TranscriptEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(base, rel))
	require.NoError(t, err)
	var entries []TranscriptEntry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry TranscriptEntry
		require.NoError(t, json.Unmarshal([]byte(line), &entry), line)
		entries = append(entries, entry)
	}
	return entries
}

func TestTaskRegistry_AllocateAndFinish(t *testing.T) {
	reg := NewTaskRegistry("turn-1", nil)
	require.NotNil(t, reg)

	assert.Equal(t, "task-1", reg.AllocateTask())
	assert.Equal(t, "task-2", reg.AllocateTask())

	reg.Finish("task-1", TaskStatusCompleted, "done")
	reg.Close()
}

func TestTaskRegistry_ProgressBroadcastsRunningSnapshot(t *testing.T) {
	var events []sessions.Event
	var durables []bool
	reg := NewTaskRegistry("turn-1", func(ev sessions.Event, durable bool) {
		events = append(events, ev)
		durables = append(durables, durable)
	})
	id := reg.AllocateTask()
	reg.RegisterTask(id, "Research fires\nmore detail", ".tasks/call-1.output", t.TempDir(), "turn-1", true)

	chunk := `{"step":"reasoning","content":"hmm"}
{"step":"tool","content":"call web_fetch {\"url\":\"https://example.org/a/very/long/url/that/keeps/going/and/going/past/the/limit/for/sure/yes\"}"}
`
	reg.Progress(id, []byte(chunk))
	require.Len(t, events, 1)
	assert.False(t, durables[0], "running progress is live-only")
	task := events[0].Task
	require.NotNil(t, task)
	assert.Equal(t, id, task.TaskID)
	assert.Equal(t, sessions.ToolStatusRunning, task.Status)
	assert.Equal(t, ".tasks/call-1.output", task.TranscriptPath)
	assert.Equal(t, 2, task.Steps)
	assert.True(t, len(task.Progress) <= progressLineLimit+len("…"))
	assert.Contains(t, task.Progress, "call web_fetch")

	// A second chunk inside the emit interval updates the record but does
	// not re-broadcast; the terminal snapshot carries no progress text.
	reg.Progress(id, []byte(`{"step":"text","content":"Found three sources"}`+"\n"))
	assert.Len(t, events, 1)
	reg.Finish(id, TaskStatusCompleted, "report")
	require.Len(t, events, 2)
	assert.True(t, durables[1])
	assert.Equal(t, sessions.ToolStatusCompleted, events[1].Task.Status)
	assert.Empty(t, events[1].Task.Progress)
	assert.Equal(t, "report", events[1].Task.Result)

	reg.Progress(id, []byte(`{"step":"text","content":"late"}`+"\n"))
	assert.Len(t, events, 2, "progress after terminal is dropped")
}

func TestTaskRegistry_TranscriptRecordsLandOnTheReservedFile(t *testing.T) {
	base := t.TempDir()
	reg := NewTaskRegistry("turn-1", nil)
	id := reg.AllocateTask()

	// The registry's own records land on the transcript file eino reserved
	// for the delegation: whatever the registry appends between the child's
	// records and the terminal line shares the child's file.
	reg.RegisterTask(id, "Research fires", ".tasks/call-7.output", base, "turn-1", true)
	reg.appendTranscript(id, TranscriptEntry{Step: StepText, Content: "interim"})

	entries := readTranscriptEntries(t, base, ".tasks/call-7.output")
	require.Len(t, entries, 1)
	assert.Equal(t, StepText, entries[0].Step)

	reg.Finish(id, TaskStatusFailed, "the child blew up")
	entries = readTranscriptEntries(t, base, ".tasks/call-7.output")
	require.Len(t, entries, 2)
	last := entries[len(entries)-1]
	assert.Equal(t, StepResult, last.Step)
	assert.Equal(t, "failed", last.Status)
	assert.Equal(t, "the child blew up", last.Content)
}

func TestSummarizeTranscriptChunk(t *testing.T) {
	summary, count := summarizeTranscriptChunk([]byte(`{"step":"task","content":"the task"}
{"step":"reasoning","content":"thinking hard"}
`))
	assert.Equal(t, 2, count)
	assert.Equal(t, "Thinking…", summary)

	summary, count = summarizeTranscriptChunk([]byte(`{"step":"tool","content":"result web_fetch: <html>"}` + "\n"))
	assert.Equal(t, 1, count)
	assert.Equal(t, "", summary, "tool results are not progress on their own")

	summary, count = summarizeTranscriptChunk([]byte("not json\n\n"))
	assert.Equal(t, 0, count)
	assert.Equal(t, "", summary)
}

func TestTaskRegistry_SpaceIsSessionScoped(t *testing.T) {
	reg := NewTaskRegistry("turn-1", nil)
	ctx := context.Background()

	mgr1, runner1, err := reg.Space(ctx, t.TempDir(), "turn-1")
	require.NoError(t, err)
	require.NotNil(t, mgr1)
	require.NotNil(t, runner1)

	mgr2, runner2, err := reg.Space(ctx, t.TempDir(), "turn-2")
	require.NoError(t, err)
	assert.Same(t, mgr1, mgr2, "a later turn reuses the session's manager")
	assert.Same(t, runner1, runner2)
	assert.Equal(t, "turn-2", reg.space.store.currentTurnID())

	reg.SetTurnID("turn-3")
	assert.Equal(t, "turn-3", reg.space.store.currentTurnID())
}

func TestNormalizeDelegationArgs(t *testing.T) {
	out := normalizeDelegationArgs(
		`{"task":"research fires in Indonesia"}`,
		GeneralPurposeSubagentName,
	)
	assert.JSONEq(t, `{
		"prompt": "research fires in Indonesia",
		"description": "research fires in Indonesia",
		"subagent_type": "general-purpose",
		"run_in_background": true
	}`, out)

	out = normalizeDelegationArgs(
		`{"subagent_type":"scout","prompt":"dig","description":"Dig","run_in_background":false}`,
		GeneralPurposeSubagentName,
	)
	assert.JSONEq(t, `{
		"subagent_type": "scout",
		"prompt": "dig",
		"description": "Dig",
		"run_in_background": true
	}`, out)

	assert.Equal(t, "not json", normalizeDelegationArgs("not json", "x"),
		"malformed arguments pass through to the tool's own validation")
}
