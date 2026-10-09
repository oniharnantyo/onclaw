package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
)

// taskProgressBuffer caps one subscriber's frame buffer. Mirrors the
// transcript subscriber buffer: an attached consumer reading at least as fast
// as the tasks produce is lossless; anything slower drops overflow instead of
// stalling the run.
const taskProgressBuffer = 128

// taskProgressPageLimit bounds one ListTaskEvents page. The drain re-pages
// each tick, so the bound trades per-tick clone cost against pagination
// depth; a task's record count is bounded by the child's own iteration cap.
const taskProgressPageLimit = 200

// taskProgressStream is one subscriber's view of a run's task progress: a
// bounded frame buffer with drop-new semantics, the EventStream contract
// carried over to frames — a slow or absent consumer is never allowed to
// stall the run's drain. Close drains to EOF.
type taskProgressStream struct {
	frames chan TaskProgressFrame
	mu     sync.Mutex
	closed bool
	drops  int
}

func newTaskProgressStream(buffer int) *taskProgressStream {
	if buffer < 1 {
		buffer = taskProgressBuffer
	}
	return &taskProgressStream{frames: make(chan TaskProgressFrame, buffer)}
}

// send enqueues one frame without ever blocking: drop-new on a full buffer,
// silent no-op after close.
func (s *taskProgressStream) send(frame TaskProgressFrame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.frames <- frame:
		return true
	default:
		s.drops++
		return false
	}
}

// recv returns the next frame, or io.EOF once the stream is closed and fully
// drained.
func (s *taskProgressStream) recv() (TaskProgressFrame, error) {
	frame, ok := <-s.frames
	if !ok {
		return TaskProgressFrame{}, io.EOF
	}
	return frame, nil
}

// Dropped reports how many frames were discarded because the buffer was full.
func (s *taskProgressStream) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drops
}

func (s *taskProgressStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.frames)
}

// taskProgressCursor is the drain's per-task state: the lane kind (enriched
// from the task snapshot on first sight) and how many records of the task's
// append-only event log have already been fanned.
type taskProgressCursor struct {
	kind string
	seen int
}

// taskProgressHub is one run's subscription registry and per-task drain
// state (add-subagent-runs-surface 2.2). The pump feeds it — task_created
// registrations and terminal transitions — and the sibling drain tick fans
// each tracked task's new records to every subscriber. One mutex orders the
// fan so a task's records always precede its terminal frame, whichever
// goroutine reaches the hub first.
type taskProgressHub struct {
	store   *bgtask.InMemoryStore
	manager *bgtask.Manager

	mu      sync.Mutex
	nextSub uint64
	subs    map[uint64]*taskProgressStream
	tasks   map[string]*taskProgressCursor
}

func newTaskProgressHub(store *bgtask.InMemoryStore, manager *bgtask.Manager) *taskProgressHub {
	return &taskProgressHub{
		store:   store,
		manager: manager,
		subs:    make(map[uint64]*taskProgressStream),
		tasks:   make(map[string]*taskProgressCursor),
	}
}

// subscribe attaches a fresh subscriber stream. It never blocks and never
// refuses while the run lives.
func (h *taskProgressHub) subscribe() (uint64, *taskProgressStream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSub++
	id := h.nextSub
	stream := newTaskProgressStream(taskProgressBuffer)
	h.subs[id] = stream
	return id, stream
}

// unsubscribe detaches one subscriber; unknown ids are no-ops.
func (h *taskProgressHub) unsubscribe(subID uint64) {
	h.mu.Lock()
	stream, ok := h.subs[subID]
	if ok {
		delete(h.subs, subID)
	}
	h.mu.Unlock()
	if ok {
		stream.close()
	}
}

// close detaches every subscriber — the run-end sweep closes all task
// fan-outs so consumers drain to EOF.
func (h *taskProgressHub) close() {
	h.mu.Lock()
	subs := make([]*taskProgressStream, 0, len(h.subs))
	for id, stream := range h.subs {
		subs = append(subs, stream)
		delete(h.subs, id)
	}
	h.mu.Unlock()
	for _, stream := range subs {
		stream.close()
	}
}

// trackedIDs snapshots the currently tracked task ids.
func (h *taskProgressHub) trackedIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.tasks))
	for id := range h.tasks {
		ids = append(ids, id)
	}
	return ids
}

// track registers a task for the drain. Idempotent: redelivered
// task_created notifications re-register the same cursor.
func (h *taskProgressHub) track(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.tasks[taskID]; !ok {
		h.tasks[taskID] = &taskProgressCursor{}
	}
}

// drainTick fans each tracked task's records past its per-task cursor. One
// shot per ticker beat — the drain goroutine owns the cadence.
func (h *taskProgressHub) drainTick(ctx context.Context) {
	for _, taskID := range h.trackedIDs() {
		h.drainTask(ctx, taskID)
	}
}

// drainTask fans one task's new records. The hub mutex is held across the
// store read and the fan so records and terminal frames interleave in fan
// order; the store read is in-memory and every send is non-blocking, so the
// critical section stays bounded.
func (h *taskProgressHub) drainTask(ctx context.Context, taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cursor, ok := h.tasks[taskID]
	if !ok {
		return
	}
	if cursor.kind == "" {
		task, err := h.manager.Get(ctx, taskID)
		if err != nil {
			if errors.Is(err, bgtask.ErrNotFound) {
				// The process-local store no longer knows the task — nothing
				// honest left to fan for it.
				delete(h.tasks, taskID)
				return
			}
			slog.WarnContext(ctx, "agents: task progress enrichment failed (best-effort)",
				"task_id", taskID, "error", err)
			return
		}
		cursor.kind = taskKindFor(task.Spec.Kind)
	}
	frames, seen, err := readTaskProgressFrames(ctx, h.store, taskID, cursor.kind, cursor.seen)
	if err != nil {
		slog.WarnContext(ctx, "agents: task progress read failed (best-effort)",
			"task_id", taskID, "error", err)
		return
	}
	cursor.seen = seen
	for _, frame := range frames {
		h.fan(frame)
	}
}

// terminal fans a task's final records, then its terminal frame, then closes
// that task's fan-out — no frames for the task flow after the terminal one.
// Called by the pump on the terminal notification, after the completion chip
// emitted; the final read runs before the terminal frame so the trajectory
// ends complete. Unknown tasks register on arrival: a terminal transition is
// itself evidence the task existed.
func (h *taskProgressHub) terminal(ctx context.Context, taskID string, payload TaskCompletedPayload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cursor, ok := h.tasks[taskID]
	if !ok {
		cursor = &taskProgressCursor{}
		h.tasks[taskID] = cursor
	}
	if cursor.kind == "" {
		cursor.kind = payload.Kind
	}
	frames, seen, err := readTaskProgressFrames(ctx, h.store, taskID, cursor.kind, cursor.seen)
	if err != nil {
		slog.WarnContext(ctx, "agents: task progress final drain failed (best-effort)",
			"task_id", taskID, "error", err)
	} else {
		for _, frame := range frames {
			h.fan(frame)
		}
		cursor.seen = seen
	}
	h.fan(TaskProgressFrame{TaskID: taskID, Kind: cursor.kind, Terminal: &payload})
	delete(h.tasks, taskID)
}

// fan fans one frame to every subscriber. Called with h.mu held; send is
// non-blocking, so a slow consumer only drops its own frames.
func (h *taskProgressHub) fan(frame TaskProgressFrame) {
	for _, stream := range h.subs {
		stream.send(frame)
	}
}

// drainTaskProgress reads each tracked task's progress past its per-task
// cursor and fans the new records to subscribers until ctx is done
// (add-subagent-runs-surface 2.2, D1 verdict). It runs as the notification
// pump's sibling at the same wiring point and cadence — task discovery rides
// the pump's task_created notifications, so this drain never touches the
// notification outbox the pump leases from. Best-effort end to end: store or
// enrichment errors log and continue, and slow subscribers drop frames
// instead of stalling the run.
func drainTaskProgress(ctx context.Context, space *BackgroundTaskSpace) {
	ticker := time.NewTicker(notificationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		space.progress.drainTick(ctx)
	}
}

// readTaskProgressFrames reads a task's event log past skip records (the
// client's or cursor's seen count) and decodes each event into record frames.
// Sequence is the 1-based ordinal of a record in the task's record stream —
// per-task monotonic from 1 in append order. A fresh empty cursor opens a new
// snapshot each call (the D1 verdict): the continuation cursors eino returns
// are snapshot-stable and never reach events appended after the first page.
// Returns the frames and the task's total record count (the new cursor).
func readTaskProgressFrames(ctx context.Context, store *bgtask.InMemoryStore, taskID, kind string, skip int) ([]TaskProgressFrame, int, error) {
	frames := make([]TaskProgressFrame, 0)
	total := 0
	cursor := ""
	for {
		page, err := store.ListTaskEvents(ctx, &bgtask.ListTaskEventsRequest{
			TaskID: taskID, Cursor: cursor, Limit: taskProgressPageLimit,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, event := range page.Events {
			for _, record := range decodeTaskProgressRecords(kind, event.Data) {
				if total >= skip {
					frames = append(frames, TaskProgressFrame{
						TaskID:   taskID,
						Kind:     kind,
						Sequence: total + 1,
						Record:   &record,
					})
				}
				total++
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return frames, total, nil
}

// DecodeTaskProgressRecords decodes one lane record payload into progress
// records — the exported decode the session tasks handler replays persisted
// output files with (the live drain uses the same decode). Delegation payloads
// are the subagent lane's TranscriptFormat JSONL records
// ({"agent_name","message"}); shell payloads are raw output text chunks. A
// payload that no longer parses falls back to a single text record carrying
// the raw bytes — the trajectory stays display-grade instead of losing lines.
func DecodeTaskProgressRecords(kind string, data []byte) []TaskProgressRecord {
	return decodeTaskProgressRecords(kind, data)
}

// decodeTaskProgressRecords maps one task-event payload onto the frame
// record vocabulary. The shell lane emits raw output chunks — one text record
// each. The delegation lane emits one TranscriptFormat record per child
// message; its content blocks expand in order into text, tool_call, and
// tool_result records.
func decodeTaskProgressRecords(kind string, data []byte) []TaskProgressRecord {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	if kind == "shell" {
		return []TaskProgressRecord{{Type: TaskProgressRecordText, Text: string(data)}}
	}
	var envelope struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil || len(envelope.Message) == 0 {
		return fallbackTextRecord(data)
	}
	var message struct {
		ContentBlocks []*struct {
			AssistantGenText *struct {
				Text string `json:"text"`
			} `json:"assistant_gen_text,omitempty"`
			UserInputText *struct {
				Text string `json:"text"`
			} `json:"user_input_text,omitempty"`
			FunctionToolCall *struct {
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function_tool_call,omitempty"`
			FunctionToolResult *struct {
				Name    string `json:"name"`
				Content []*struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"function_tool_result,omitempty"`
		} `json:"content_blocks"`
	}
	if err := json.Unmarshal(envelope.Message, &message); err != nil {
		return fallbackTextRecord(data)
	}
	records := make([]TaskProgressRecord, 0, len(message.ContentBlocks))
	for _, block := range message.ContentBlocks {
		switch {
		case block == nil:
			continue
		case block.FunctionToolCall != nil:
			records = append(records, TaskProgressRecord{
				Type: TaskProgressRecordToolCall,
				Name: block.FunctionToolCall.Name,
				Args: block.FunctionToolCall.Arguments,
			})
		case block.FunctionToolResult != nil:
			var text bytes.Buffer
			for _, part := range block.FunctionToolResult.Content {
				if part != nil {
					text.WriteString(part.Text)
				}
			}
			records = append(records, TaskProgressRecord{
				Type:   TaskProgressRecordToolResult,
				Name:   block.FunctionToolResult.Name,
				Result: text.String(),
			})
		default:
			line := ""
			if block.AssistantGenText != nil {
				line = block.AssistantGenText.Text
			} else if block.UserInputText != nil {
				line = block.UserInputText.Text
			}
			if line != "" {
				records = append(records, TaskProgressRecord{Type: TaskProgressRecordText, Text: line})
			}
		}
	}
	if len(records) == 0 {
		return fallbackTextRecord(data)
	}
	return records
}

// fallbackTextRecord is the decode fallback for payloads that no longer match
// the lane shapes: the raw bytes stay visible instead of vanishing.
func fallbackTextRecord(data []byte) []TaskProgressRecord {
	return []TaskProgressRecord{{Type: TaskProgressRecordText, Text: string(data)}}
}
