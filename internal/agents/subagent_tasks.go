package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The delegated-task registry owns one turn's task-ID space (spec
// agent-subagents "Background lane": task identifiers are unique within the
// run). Tasks live for the parent run: the registry is built per turn and
// closing it cancels every still-running child, so no background delegation
// outlives the turn that spawned it.
//
// Task identity is minted here, not by the store or the model: "task-1",
// "task-2", ... in delegation order, so the same identifier addresses the
// task in the agent tool's result, the task_activity events, the
// task_output/task_stop tools, and the materialized transcript file.

// Delegated-task states. They reuse the tool-activity status vocabulary so
// the transcript has one status alphabet: running, completed, failed, stopped.
const (
	// taskStatusRunning marks a child turn still executing.
	taskStatusRunning = sessions.ToolStatusRunning
	// taskStatusCompleted marks a child turn that finished with an answer.
	taskStatusCompleted = sessions.ToolStatusCompleted
	// taskStatusFailed marks a child turn that ended in an error.
	taskStatusFailed = sessions.ToolStatusFailed
	// taskStatusStopped marks a child turn cancelled through task_stop or by
	// its parent turn ending.
	taskStatusStopped = sessions.ToolStatusStopped
)

// ErrTaskNotFound is the typed not-found a task_output/task_stop call gets
// for an identifier outside the run's task-ID space (an unknown id, or a
// task of a different run).
var ErrTaskNotFound = errors.New("agents: no such task in this run")

// ErrTaskNotRunning is the typed conflict a task_stop call gets for a task
// that already reached a terminal state.
var ErrTaskNotRunning = errors.New("agents: task is not running")

// TaskRecord is one delegated task's live state. Every field is guarded by
// the owning registry's mutex; snapshots cross the boundary as values.
type TaskRecord struct {
	// ID is the run-scoped task identifier ("task-1").
	ID string
	// Description is the delegated task text.
	Description string
	// Status is the task's lifecycle state (the taskStatus* constants).
	Status sessions.ToolStatus
	// Result is the child's final answer once it reached a terminal state.
	Result string
	// TranscriptPath is the workspace-relative transcript path
	// (".tasks/task-1.md"), empty when the task has no workspace to write to.
	TranscriptPath string
	// Background reports whether the task runs detached from the parent turn.
	Background bool
	// TurnID is the parent turn that spawned the task; its terminal record
	// carries this identity no matter which turn is recording when it lands.
	TurnID string

	// cancel stops the child run; set while the task is running.
	cancel context.CancelFunc
	// done closes when the child run's goroutine finished, so the registry
	// can drain completions before the turn hands back.
	done chan struct{}
	// transcript is the task's materialized transcript writer, set while the
	// child run holds it (see delegation.startTranscript).
	transcript *taskTranscript
}

// Terminal reports whether the task reached a state that will not change
// again: completed, failed, or stopped. Only a running task is non-terminal.
func (t *TaskRecord) Terminal() bool {
	return t == nil || t.Status != taskStatusRunning
}

// snapshot renders the record as the transcript's task payload. The
// occurrence is stamped by the caller.
func (t TaskRecord) snapshot() *sessions.TaskActivity {
	return &sessions.TaskActivity{
		TaskID:         t.ID,
		Description:    taskSummary(t.Description),
		Status:         t.Status,
		Result:         t.Result,
		TranscriptPath: t.TranscriptPath,
		Background:     t.Background,
	}
}

// taskSummary derives the one-line human summary of a delegated task: its
// first non-empty line, trimmed and bounded.
func taskSummary(description string) string {
	description = strings.TrimSpace(description)
	if line, _, found := strings.Cut(description, "\n"); found {
		description = line
	}
	description = strings.TrimSpace(description)
	const maxSummaryLen = 120
	if len(description) > maxSummaryLen {
		description = strings.TrimSpace(description[:maxSummaryLen])
	}
	return description
}

// taskRegistry is one run's delegated-task space: the task-ID counter, every
// task's live state, and the record callback terminal completion is surfaced
// through.
//
// Task lifetime is the run's, not the turn context's: the registry owns a
// detached context it cancels only in close, so a background child is never
// torn down behind the registry's back (its cancellation is made visible as
// the stopped state by the one owner). close is therefore the single place
// a run's children are stopped, and it always runs before the parent turn
// returns.
type taskRegistry struct {
	// runCtx parents every task's context; close cancels it.
	runCtx context.Context
	// cancelCtx cancels runCtx when the registry closes.
	cancelCtx context.CancelFunc
	// record persists (when durable) and broadcasts one parent-session event
	// — the turn's own record closure, so a background completion lands on the
	// parent transcript exactly as an inline event would.
	record recordFn

	mu     sync.Mutex
	next   int
	tasks  []*TaskRecord
	tasksB map[string]*TaskRecord
	closed bool

	currentDesc     string
	currentBg       bool
	currentTurnID   string
	latestAllocated string
}

// newTaskRegistry returns the run's empty task space over a detached context
// and the parent turn's event recorder.
func newTaskRegistry(record recordFn) *taskRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &taskRegistry{runCtx: ctx, cancelCtx: cancel, record: record, tasksB: make(map[string]*TaskRecord)}
}

// setPending registers the parameters of an upcoming task allocation.
func (r *taskRegistry) setPending(description string, background bool, turnID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentDesc = description
	r.currentBg = background
	r.currentTurnID = turnID
}

// allocateTask mints the next task ID and creates its record and transcript.
func (r *taskRegistry) allocateTask(tasksDir string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id := fmt.Sprintf("task-%d", r.next)
	r.latestAllocated = id

	rec := &TaskRecord{
		ID:          id,
		Description: r.currentDesc,
		Status:      taskStatusRunning,
		Background:  r.currentBg,
		TurnID:      r.currentTurnID,
		done:        make(chan struct{}),
	}
	_, cancel := context.WithCancel(r.runCtx)
	rec.cancel = cancel
	r.tasks = append(r.tasks, rec)
	r.tasksB[id] = rec

	if tasksDir != "" {
		transcript, err := newTaskTranscript(tasksDir, rec.ID, rec.Description)
		if err == nil {
			rec.transcript = transcript
			rec.TranscriptPath = filepath.Join(workspaces.DirTasks, rec.ID+taskTranscriptSuffix)
		}
	}
	snapshot := *rec
	go r.emitRunning(snapshot)
	return id
}

// latestAllocatedID returns the most recently allocated task ID.
func (r *taskRegistry) latestAllocatedID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latestAllocated
}

// nextID mints the next sequential task ID for the session.
func (r *taskRegistry) nextID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	return fmt.Sprintf("task-%d", r.next)
}

// latestTaskID returns the ID of the most recently created task, or empty if none.
func (r *taskRegistry) latestTaskID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tasks) == 0 {
		return ""
	}
	return r.tasks[len(r.tasks)-1].ID
}

// recordTask records a newly-spawned task by ID and description.
func (r *taskRegistry) recordTask(id, description string, background bool, turnID string) *TaskRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.tasksB[id]; ok {
		return rec
	}
	rec := &TaskRecord{
		ID:          id,
		Description: description,
		Status:      taskStatusRunning,
		Background:  background,
		TurnID:      turnID,
		done:        make(chan struct{}),
	}
	_, cancel := context.WithCancel(r.runCtx)
	rec.cancel = cancel
	r.tasks = append(r.tasks, rec)
	r.tasksB[rec.ID] = rec
	return rec
}

// activeTranscript returns the active non-terminal task's transcript, if any.
func (r *taskRegistry) activeTranscript() *taskTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.tasks) - 1; i >= 0; i-- {
		if !r.tasks[i].Terminal() && r.tasks[i].transcript != nil {
			return r.tasks[i].transcript
		}
	}
	return nil
}

// spawn registers a new task and returns its record plus the child context
// its run is bounded by. Registration mints the identifier, so identifiers
// are unique within the session's task space by construction.
func (r *taskRegistry) spawn(description string, background bool, turnID string) (*TaskRecord, context.Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, nil, errors.New("agents: the run's task space is closed")
	}

	r.next++
	rec := &TaskRecord{
		ID:          fmt.Sprintf("task-%d", r.next),
		Description: description,
		Status:      taskStatusRunning,
		Background:  background,
		TurnID:      turnID,
		done:        make(chan struct{}),
	}
	childCtx, cancel := context.WithCancel(r.runCtx)
	rec.cancel = cancel

	r.tasks = append(r.tasks, rec)
	r.tasksB[rec.ID] = rec
	return rec, childCtx, nil
}

// lookup returns a task's current snapshot. Unknown identifiers — including
// tasks of another run — are ErrTaskNotFound.
func (r *taskRegistry) lookup(id string) (TaskRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tasksB[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("task %q: %w", id, ErrTaskNotFound)
	}
	return *rec, nil
}

// setTranscriptPath records the workspace-relative transcript location once
// the writer exists, so the terminal transcript event can carry it.
func (r *taskRegistry) setTranscriptPath(id, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.tasksB[id]; ok {
		rec.TranscriptPath = path
	}
}

// setTranscript attaches the task's transcript writer to the record, so the
// child run's drain can write through it.
func (r *taskRegistry) setTranscript(id string, transcript *taskTranscript) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.tasksB[id]; ok {
		rec.transcript = transcript
	}
}

// takeTranscript hands the task's transcript writer to the child run and
// clears the slot, so exactly one owner closes it.
func (r *taskRegistry) takeTranscript(id string) *taskTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tasksB[id]
	if !ok {
		return nil
	}
	transcript := rec.transcript
	rec.transcript = nil
	return transcript
}

// setRecord installs the calling turn's event recorder (the per-session busy
// guard serializes turns, so each install is race-free). The previous
// recorder stays valid — its context is cancellation-proof — but subsequent
// events flow through the newest one.
func (r *taskRegistry) setRecord(record recordFn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record = record
}

// emitTask records one task event through the current recorder, stamping the
// SPAWNING turn's identity: a background task completing after later turns
// began still lands on the transcript as its own turn's task.
func (r *taskRegistry) emitTask(rec TaskRecord, durable bool) {
	r.mu.Lock()
	record := r.record
	r.mu.Unlock()
	if record == nil {
		return
	}
	ev := sessions.Event{Kind: sessions.KindTaskActivity, Task: rec.snapshot()}
	ev.TurnID = rec.TurnID
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = now()
	}
	// Best-effort, like every live event: the transcript's completion record
	// must never turn a finished child run into a failed turn.
	_ = record(&ev, durable)
}

// finish lands a task's terminal state and surfaces it on the parent
// transcript (spec agent-subagents "Task completion surfacing"): the
// terminal task_activity record is persisted, which broadcasts it live to
// the chat view. Only the first terminal transition wins, so a cancel racing
// a natural completion reports one outcome.
func (r *taskRegistry) finish(id string, status sessions.ToolStatus, result string) TaskRecord {
	r.mu.Lock()
	rec, ok := r.tasksB[id]
	if !ok {
		r.mu.Unlock()
		return TaskRecord{}
	}
	if rec.Terminal() {
		// The first terminal transition won — a cancel racing a natural
		// completion reports the landed state unchanged.
		snapshot := *rec
		r.mu.Unlock()
		return snapshot
	}
	rec.Status = status
	rec.Result = result
	rec.cancel = nil
	close(rec.done)
	if rec.transcript != nil {
		rec.transcript.terminal(string(status), result)
		rec.transcript = nil
	}
	snapshot := *rec
	r.mu.Unlock()

	r.emitTask(snapshot, true)
	return snapshot
}

// emitRunning broadcasts a task's running record. Running is live-only
// (design D10): the record rides the live channel so the chat view can show
// the task card immediately, and the terminal record replaces it in the
// transcript.
func (r *taskRegistry) emitRunning(rec TaskRecord) {
	r.emitTask(rec, false)
}

// cancel stops one task by identifier: it cancels the child's context and
// reports the task stopped. A terminal task is ErrTaskNotRunning; an unknown
// one is ErrTaskNotFound. The caller is responsible for waiting on done when
// it needs the cancellation to have taken effect.
func (r *taskRegistry) cancel(id string) (TaskRecord, error) {
	r.mu.Lock()
	rec, ok := r.tasksB[id]
	if !ok {
		r.mu.Unlock()
		return TaskRecord{}, fmt.Errorf("task %q: %w", id, ErrTaskNotFound)
	}
	if rec.Terminal() {
		r.mu.Unlock()
		return TaskRecord{}, fmt.Errorf("task %q: %w", id, ErrTaskNotRunning)
	}

	cancel := rec.cancel
	rec.Status = taskStatusStopped
	rec.cancel = nil
	close(rec.done)
	if rec.transcript != nil {
		rec.transcript.terminal(string(taskStatusStopped), "")
		rec.transcript = nil
	}
	snapshot := *rec
	if cancel != nil {
		cancel()
	}
	r.mu.Unlock()

	r.emitTask(snapshot, true)
	return snapshot, nil
}

// close ends the run's task space: every still-running task is cancelled and
// awaited, so the turn returns only once no child goroutine can still touch
// the session. Idempotent.
func (r *taskRegistry) close() {
	r.mu.Lock()
	r.closed = true
	pending := make([]*TaskRecord, 0, len(r.tasks))
	for _, rec := range r.tasks {
		if rec.Terminal() {
			continue
		}
		pending = append(pending, rec)
	}
	r.mu.Unlock()

	for _, rec := range pending {
		r.cancel(rec.ID)
	}
	for _, rec := range pending {
		<-rec.done
	}
	// The run context is released last: every task already reached a
	// terminal state through the explicit cancel above, so nothing observes
	// this cancellation.
	r.cancelCtx()
}

// wait blocks until every running task reached a terminal state or the
// timeout elapsed, returning false on timeout. It exists for tests and for
// foreground delegation, where the caller already holds done for the one
// task it spawned.
func (r *taskRegistry) wait(d time.Duration) bool {
	r.mu.Lock()
	pending := make([]*TaskRecord, 0, len(r.tasks))
	for _, rec := range r.tasks {
		if !rec.Terminal() {
			pending = append(pending, rec)
		}
	}
	r.mu.Unlock()

	deadline := time.After(d)
	for _, rec := range pending {
		select {
		case <-rec.done:
		case <-deadline:
			return false
		}
	}
	return true
}
