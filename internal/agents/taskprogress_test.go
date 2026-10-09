package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
)

// ---- 2.3: wire round-trip ----

// TestTaskProgressFrameRoundTrip is 2.3's verification: the frame and record
// shapes marshal and unmarshal losslessly, the JSON keys are the pinned wire
// contract (task_id/kind/sequence/record/terminal), and the omitempty
// variants collapse to exactly one payload branch per frame.
func TestTaskProgressFrameRoundTrip(t *testing.T) {
	recordFrame := TaskProgressFrame{
		TaskID:   "task_1",
		Kind:     "delegation",
		Sequence: 7,
		Record: &TaskProgressRecord{
			Type: TaskProgressRecordToolCall,
			Name: "files.write",
			Args: `{"path":"a.txt"}`,
		},
	}
	data, err := json.Marshal(recordFrame)
	if err != nil {
		t.Fatalf("marshal record frame: %v", err)
	}
	var back TaskProgressFrame
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal record frame: %v", err)
	}
	if back.TaskID != recordFrame.TaskID || back.Kind != recordFrame.Kind ||
		back.Sequence != recordFrame.Sequence || back.Record == nil ||
		*back.Record != *recordFrame.Record || back.Terminal != nil {
		t.Fatalf("record frame round-trip mismatch: %+v vs %+v", back, recordFrame)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"task_id", "kind", "sequence", "record"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("record frame JSON missing pinned key %q: %s", key, data)
		}
	}
	if _, ok := keys["terminal"]; ok {
		t.Fatalf("record frame JSON must omit terminal: %s", data)
	}
	var recordKeys map[string]json.RawMessage
	if err := json.Unmarshal(keys["record"], &recordKeys); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if _, ok := recordKeys["type"]; !ok {
		t.Fatalf("record JSON missing pinned key \"type\": %s", keys["record"])
	}
	if _, ok := recordKeys["text"]; ok {
		t.Fatalf("record JSON must omit empty text: %s", keys["record"])
	}

	terminalFrame := TaskProgressFrame{
		TaskID:   "task_1",
		Kind:     "shell",
		Sequence: 0,
		Terminal: &TaskCompletedPayload{
			TaskID:     "task_1",
			Kind:       "shell",
			Outcome:    "canceled",
			OutputPath: TasksOutputDir + "/task_1.output",
			Summary:    "long sleep",
		},
	}
	data, err = json.Marshal(terminalFrame)
	if err != nil {
		t.Fatalf("marshal terminal frame: %v", err)
	}
	back = TaskProgressFrame{} // unmarshal merges; reset before the second read
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal terminal frame: %v", err)
	}
	if back.Record != nil || back.Terminal == nil || *back.Terminal != *terminalFrame.Terminal {
		t.Fatalf("terminal frame round-trip mismatch: %+v vs %+v", back, terminalFrame)
	}
	keys = nil
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("unmarshal terminal into map: %v", err)
	}
	if _, ok := keys["record"]; ok {
		t.Fatalf("terminal frame JSON must omit record: %s", data)
	}
	if _, ok := keys["terminal"]; !ok {
		t.Fatalf("terminal frame JSON missing pinned key \"terminal\": %s", data)
	}
}

// ---- 2.2: the drain ----

// frameCollector gathers progress frames from one subscriber stream.
type frameCollector struct {
	mu     sync.Mutex
	frames []TaskProgressFrame
}

func (c *frameCollector) collect(stream *taskProgressStream) {
	for {
		frame, err := stream.recv()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.frames = append(c.frames, frame)
		c.mu.Unlock()
	}
}

func (c *frameCollector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

func (c *frameCollector) terminals() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, frame := range c.frames {
		if frame.Terminal != nil {
			n++
		}
	}
	return n
}

func (c *frameCollector) snapshot() []TaskProgressFrame {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]TaskProgressFrame(nil), c.frames...)
}

// delegationRecord builds one delegation-lane TranscriptFormat payload: the
// JSONL record shape the subagent middleware emits via EmitProgress
// ({"agent_name","message"} with the child message's content blocks).
func delegationRecord(text string) []byte {
	payload, err := json.Marshal(map[string]any{
		"agent_name": "general-purpose",
		"message": map[string]any{
			"role": "assistant",
			"content_blocks": []map[string]any{
				{"type": "assistant_gen_text", "assistant_gen_text": map[string]any{"text": text}},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return payload
}

// TestTaskProgressDrain_BothLanes is 2.2's verification: with the pump and
// the drain running on a real task space, one delegation task and one shell
// task fan their records to subscribers with per-task monotonic sequences in
// append order, each task's frames are keyed by its own id and lane kind, and
// every task ends with exactly one terminal frame carrying the completion
// payload — after which no further frames flow for that task.
func TestTaskProgressDrain_BothLanes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}

	subID, stream := space.progress.subscribe()
	if subID == 0 || stream == nil {
		t.Fatalf("bad subscription handle (id=%d, stream=%v)", subID, stream)
	}
	var collector frameCollector
	go collector.collect(stream)
	defer space.progress.close()

	delegationDone := make(chan string, 1)
	go func() {
		task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
			Kind:            subagent.TaskKindSubagent,
			Description:     "research sweep",
			SessionID:       "sess_drain",
			NotifySession:   true,
			RunInBackground: true,
		}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
			for _, text := range []string{"scanning the corpus", "drafting the summary"} {
				if _, err := runtime.EmitProgress(ctx, "", delegationRecord(text)); err != nil {
					return "", err
				}
			}
			return "report", nil
		})
		if err != nil {
			t.Errorf("delegation Runner.Run: %v", err)
			return
		}
		delegationDone <- task.Spec.ID
	}()

	shellDone := make(chan string, 1)
	go func() {
		task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
			Kind:            einoTaskKindBash,
			Description:     "nightly build",
			SessionID:       "sess_drain",
			NotifySession:   true,
			RunInBackground: true,
		}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
			for _, chunk := range []string{"compiling ", "modules\n"} {
				if _, err := runtime.EmitProgress(ctx, "", []byte(chunk)); err != nil {
					return "", err
				}
			}
			return "build ok", nil
		})
		if err != nil {
			t.Errorf("shell Runner.Run: %v", err)
			return
		}
		shellDone <- task.Spec.ID
	}()

	delegationID, shellID := <-delegationDone, <-shellDone
	waitForTaskStatus(t, space, delegationID, bgtask.StatusCompleted)
	waitForTaskStatus(t, space, shellID, bgtask.StatusCompleted)

	// The pump registers and terminalizes; the drain fans records. Both run
	// exactly as the production wiring starts them.
	pumpStop := runPump(ctx, space, &emissionCollector{})
	defer pumpStop()
	drainStop := runDrain(ctx, space)
	defer drainStop()

	deadline := time.Now().Add(10 * time.Second)
	for (collector.terminals() < 2 || collector.len() < 6) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if collector.terminals() != 2 {
		t.Fatalf("expected 2 terminal frames, got %d (frames: %+v)", collector.terminals(), collector.snapshot())
	}

	// One more drain beat: no records may arrive after a task's terminal, and
	// the totals must hold steady.
	time.Sleep(2*notificationPollInterval + 100*time.Millisecond)
	frames := collector.snapshot()
	if collector.terminals() != 2 {
		t.Fatalf("terminal frames after settle = %d, want exactly 2", collector.terminals())
	}

	byTask := map[string][]TaskProgressFrame{}
	for _, frame := range frames {
		byTask[frame.TaskID] = append(byTask[frame.TaskID], frame)
	}
	if len(byTask) != 2 {
		t.Fatalf("expected frames keyed to exactly 2 tasks, got %d (%+v)", len(byTask), frames)
	}

	assertTaskTrajectory := func(name, taskID, kind string, wantTexts []string) {
		t.Helper()
		taskFrames := byTask[taskID]
		if len(taskFrames) == 0 {
			t.Fatalf("%s: no frames for task %q", name, taskID)
		}
		records := 0
		for i, frame := range taskFrames {
			if frame.TaskID != taskID {
				t.Fatalf("%s: frame %d carries task %q", name, i, frame.TaskID)
			}
			if frame.Kind != kind {
				t.Fatalf("%s: frame %d kind = %q, want %q", name, i, frame.Kind, kind)
			}
			if frame.Record != nil {
				if frame.Sequence != records+1 {
					t.Fatalf("%s: frame %d sequence = %d, want %d (per-task monotonic from 1)",
						name, i, frame.Sequence, records+1)
				}
				records++
				continue
			}
			if frame.Terminal == nil {
				t.Fatalf("%s: frame %d carries neither record nor terminal", name, i)
			}
			// The terminal frame is last: no frames follow it for this task.
			if i != len(taskFrames)-1 {
				t.Fatalf("%s: terminal frame at %d of %d — frames follow the terminal", name, i, len(taskFrames))
			}
			if frame.Terminal.TaskID != taskID || frame.Terminal.Kind != kind || frame.Terminal.Outcome != "completed" {
				t.Fatalf("%s: terminal payload mismatch: %+v", name, frame.Terminal)
			}
		}
		if records != len(wantTexts) {
			t.Fatalf("%s: %d record frames, want %d (frames: %+v)", name, records, len(wantTexts), taskFrames)
		}
		for i, want := range wantTexts {
			record := taskFrames[i].Record
			if record == nil || record.Type != TaskProgressRecordText || record.Text != want {
				t.Fatalf("%s: record %d = %+v, want text %q in append order", name, i, record, want)
			}
		}
	}

	assertTaskTrajectory("delegation", delegationID, "delegation", []string{"scanning the corpus", "drafting the summary"})
	assertTaskTrajectory("shell", shellID, "shell", []string{"compiling ", "modules\n"})
}

// runDrain starts the progress drain on its own goroutine and returns its
// stopper — the pump's sibling at the production wiring shape.
func runDrain(ctx context.Context, space *BackgroundTaskSpace) func() {
	drainCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		drainTaskProgress(drainCtx, space)
	}()
	return func() {
		stop()
		<-done
	}
}

// TestTaskProgressStream_DropNewSemantics: the subscriber stream never blocks
// its producer — a full buffer drops the new frame and counts it, close drains
// to EOF, and sends after close are silent no-ops.
func TestTaskProgressStream_DropNewSemantics(t *testing.T) {
	stream := newTaskProgressStream(1)
	frame := TaskProgressFrame{TaskID: "t", Kind: "shell", Sequence: 1,
		Record: &TaskProgressRecord{Type: TaskProgressRecordText, Text: "x"}}
	if !stream.send(frame) {
		t.Fatal("send into an empty buffer must succeed")
	}
	if stream.send(frame) {
		t.Fatal("send into a full buffer must drop and report false")
	}
	if stream.Dropped() != 1 {
		t.Fatalf("drops = %d, want 1", stream.Dropped())
	}
	stream.close()
	got, err := stream.recv()
	if err != nil {
		t.Fatalf("recv after close must drain the buffer: %v", err)
	}
	if got.Sequence != 1 {
		t.Fatalf("drained frame sequence = %d, want 1", got.Sequence)
	}
	if _, err := stream.recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("second recv after drain = %v, want io.EOF", err)
	}
	if stream.send(frame) {
		t.Fatal("send after close must be a no-op reporting false")
	}
	if stream.Dropped() != 1 {
		t.Fatalf("post-close sends must not count as drops, got %d", stream.Dropped())
	}
}

// TestReadTaskProgressFrames_Cursor: the record reader's skip cursor returns
// only records past the seen count, with sequences that keep the per-task
// monotonic numbering, and an over-large skip yields nothing.
func TestReadTaskProgressFrames_Cursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}
	task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
		Kind:            fsmw.ExecuteTaskKind,
		Description:     "cursor probe",
		SessionID:       "sess_cursor",
		NotifySession:   true,
		RunInBackground: true,
	}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		for i := range 3 {
			if _, err := runtime.EmitProgress(ctx, "", []byte(fmt.Sprintf("line-%d", i))); err != nil {
				return "", err
			}
		}
		return "done", nil
	})
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	waitForTaskStatus(t, space, task.Spec.ID, bgtask.StatusCompleted)

	frames, seen, err := readTaskProgressFrames(ctx, space.Store, task.Spec.ID, "shell", 0)
	if err != nil {
		t.Fatalf("read from zero: %v", err)
	}
	if len(frames) != 3 || seen != 3 {
		t.Fatalf("read from zero = %d frames, seen %d; want 3/3", len(frames), seen)
	}
	for i, frame := range frames {
		if frame.Sequence != i+1 || frame.Record == nil || frame.Record.Text != fmt.Sprintf("line-%d", i) {
			t.Fatalf("frame %d = %+v, want sequence %d text line-%d", i, frame, i+1, i)
		}
	}

	frames, seen, err = readTaskProgressFrames(ctx, space.Store, task.Spec.ID, "shell", 1)
	if err != nil {
		t.Fatalf("read past one: %v", err)
	}
	if len(frames) != 2 || seen != 3 {
		t.Fatalf("read past one = %d frames, seen %d; want 2/3", len(frames), seen)
	}
	if frames[0].Sequence != 2 || frames[0].Record.Text != "line-1" {
		t.Fatalf("first frame past the cursor = %+v, want sequence 2 text line-1", frames[0])
	}

	frames, seen, err = readTaskProgressFrames(ctx, space.Store, task.Spec.ID, "shell", 99)
	if err != nil {
		t.Fatalf("read past end: %v", err)
	}
	if len(frames) != 0 || seen != 3 {
		t.Fatalf("over-large skip = %d frames, seen %d; want 0/3", len(frames), seen)
	}

	if _, _, err := readTaskProgressFrames(ctx, space.Store, "task_none", "shell", 0); !errors.Is(err, bgtask.ErrNotFound) {
		t.Fatalf("unknown task err = %v, want ErrNotFound", err)
	}
}

// TestDecodeTaskProgressRecords: the decode maps the delegation lane's
// TranscriptFormat record onto text/tool_call/tool_result records in block
// order, raw shell chunks onto text records, and unparseable payloads onto a
// visible text fallback instead of dropping them.
func TestDecodeTaskProgressRecords(t *testing.T) {
	delegation := []byte(`{"agent_name":"general-purpose","message":{"role":"assistant","content_blocks":[` +
		`{"type":"assistant_gen_text","assistant_gen_text":{"text":"checking the index"}},` +
		`{"type":"function_tool_call","function_tool_call":{"call_id":"call-1","name":"files.search","arguments":"{\"q\":\"onclaw\"}"}},` +
		`{"type":"function_tool_result","function_tool_result":{"call_id":"call-1","name":"files.search","content":[{"type":"text","text":"3 hits"}]}}` +
		`]}}`)
	records := decodeTaskProgressRecords("delegation", delegation)
	if len(records) != 3 {
		t.Fatalf("delegation decode = %d records, want 3: %+v", len(records), records)
	}
	if records[0].Type != TaskProgressRecordText || records[0].Text != "checking the index" {
		t.Fatalf("record 0 = %+v, want text record", records[0])
	}
	if records[1].Type != TaskProgressRecordToolCall || records[1].Name != "files.search" || records[1].Args != `{"q":"onclaw"}` {
		t.Fatalf("record 1 = %+v, want tool_call record", records[1])
	}
	if records[2].Type != TaskProgressRecordToolResult || records[2].Name != "files.search" || records[2].Result != "3 hits" {
		t.Fatalf("record 2 = %+v, want tool_result record", records[2])
	}

	// Trailing newline (the lane appends one) must not break the decode.
	if got := decodeTaskProgressRecords("delegation", append(delegation, '\n')); len(got) != 3 {
		t.Fatalf("delegation decode with trailing newline = %d records, want 3", len(got))
	}

	shell := decodeTaskProgressRecords("shell", []byte("compiling "))
	if len(shell) != 1 || shell[0].Type != TaskProgressRecordText || shell[0].Text != "compiling " {
		t.Fatalf("shell decode = %+v, want one raw text record", shell)
	}

	fallback := decodeTaskProgressRecords("delegation", []byte("not json at all"))
	if len(fallback) != 1 || fallback[0].Type != TaskProgressRecordText || !strings.Contains(fallback[0].Text, "not json") {
		t.Fatalf("malformed delegation decode = %+v, want raw text fallback", fallback)
	}

	if got := decodeTaskProgressRecords("shell", []byte("")); got != nil {
		t.Fatalf("empty shell payload = %+v, want nil", got)
	}
}

// TestBackgroundTaskSpace_CancelTask: a running task accepts the cancel — the
// entry reports the post-request snapshot and the task reaches canceled —
// while unknown ids and already-terminal tasks collapse into the typed
// ErrTaskNotRunning conflict.
func TestBackgroundTaskSpace_CancelTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}

	if _, err := space.cancelTask(ctx, "task_none"); !errors.Is(err, ErrTaskNotRunning) {
		t.Fatalf("cancel unknown task err = %v, want ErrTaskNotRunning", err)
	}

	task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
		Kind:            fsmw.ExecuteTaskKind,
		Description:     "long sleep",
		SessionID:       "sess_cancel",
		NotifySession:   true,
		RunInBackground: true,
	}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := space.Manager.Get(ctx, task.Spec.ID)
		if err == nil && snapshot.Status == bgtask.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task never reached running")
		}
		time.Sleep(10 * time.Millisecond)
	}

	entry, err := space.cancelTask(ctx, task.Spec.ID)
	if err != nil {
		t.Fatalf("cancelTask: %v", err)
	}
	if entry.TaskID != task.Spec.ID || entry.Kind != "shell" {
		t.Fatalf("cancel acceptance = %+v, want the canceled shell task entry", entry)
	}
	// The process-local Manager waits for the attempt's terminal
	// acknowledgement, so the acceptance already carries the outcome.
	if entry.Status != taskStatusCanceled {
		t.Fatalf("cancel acceptance status = %q, want canceled", entry.Status)
	}
	if entry.Description != "long sleep" || entry.Summary != "long sleep" {
		t.Fatalf("cancel acceptance description/summary = %q/%q, want the task description", entry.Description, entry.Summary)
	}
	waitForTaskStatus(t, space, task.Spec.ID, bgtask.StatusCanceled)

	if _, err := space.cancelTask(ctx, task.Spec.ID); !errors.Is(err, ErrTaskNotRunning) {
		t.Fatalf("cancel terminal task err = %v, want ErrTaskNotRunning", err)
	}
}

// TestBackgroundTaskSpace_LiveTasks: the live snapshot mirrors the tracked
// tasks' authoritative store state with the lane kind mapped and the entry
// fields populated.
func TestBackgroundTaskSpace_LiveTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}
	if got := space.liveTasks(ctx); len(got) != 0 {
		t.Fatalf("empty space liveTasks = %+v, want empty", got)
	}

	task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
		Kind:            subagent.TaskKindSubagent,
		Description:     "research sweep\nwith detail lines",
		SessionID:       "sess_live",
		NotifySession:   true,
		RunInBackground: true,
	}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		return "report", nil
	})
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	// The pump discovers tasks; without it running, registration is manual —
	// the same state the pump's task_created processing produces.
	space.progress.track(task.Spec.ID)

	deadline := time.Now().Add(5 * time.Second)
	for {
		live := space.liveTasks(ctx)
		if len(live) == 1 && live[0].Status == string(bgtask.StatusCompleted) {
			if live[0].TaskID != task.Spec.ID {
				t.Fatalf("live task id = %q, want %q", live[0].TaskID, task.Spec.ID)
			}
			if live[0].Kind != "delegation" {
				t.Fatalf("live task kind = %q, want \"delegation\"", live[0].Kind)
			}
			if live[0].Description != "research sweep\nwith detail lines" {
				t.Fatalf("live task description = %q, want the full task description", live[0].Description)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("live snapshot never settled: %+v", space.liveTasks(ctx))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
