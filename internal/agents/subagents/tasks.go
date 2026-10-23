package subagents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

const (
	TaskStatusRunning   = sessions.ToolStatusRunning
	TaskStatusCompleted = sessions.ToolStatusCompleted
	TaskStatusFailed    = sessions.ToolStatusFailed
	TaskStatusStopped   = sessions.ToolStatusStopped
)

// progressEmitInterval bounds how often one running task's live snapshot is
// re-broadcast as its transcript grows; the terminal snapshot is never held.
const progressEmitInterval = 400 * time.Millisecond

// progressLineLimit bounds the one-line activity summary carried on a running
// snapshot.
const progressLineLimit = 96

var ErrTaskNotFound = errors.New("subagents: no such task in this run")
var ErrTaskNotRunning = errors.New("subagents: task is not running")

type TaskRecord struct {
	ID          string
	Description string
	Status      sessions.ToolStatus
	Result      string
	// TranscriptPath is the workspace-relative transcript file eino reserved
	// for the delegation (".tasks/<tool-call-id>.output"); the model is told
	// the same path by task_output.
	TranscriptPath string
	Background     bool
	TurnID         string
	// Progress is the one-line summary of the child's latest transcript record;
	// Steps counts the records so far. Both are display state for the running
	// form only.
	Progress string
	Steps    int

	Cancel context.CancelFunc
	Done   chan struct{}

	lastProgressEmit time.Time
	// transcriptAbs is the host path of TranscriptPath; empty without one.
	transcriptAbs string
}

func (t *TaskRecord) Terminal() bool {
	return t == nil || t.Status != TaskStatusRunning
}

func (t TaskRecord) Snapshot() *sessions.TaskActivity {
	snap := &sessions.TaskActivity{
		TaskID:         t.ID,
		Description:    TaskSummary(t.Description),
		Status:         t.Status,
		Result:         t.Result,
		TranscriptPath: t.TranscriptPath,
		Background:     t.Background,
	}
	if t.Status == TaskStatusRunning {
		snap.Progress = t.Progress
		snap.Steps = t.Steps
	}
	return snap
}

func TaskSummary(desc string) string {
	for _, line := range strings.Split(desc, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if len(trimmed) > 80 {
				return trimmed[:77] + "..."
			}
			return trimmed
		}
	}
	return "subagent task"
}

// TaskRegistry is one session's delegated-task space: the records every turn
// of the session shares, and — once a turn needs delegation — the eino
// background-task Manager and local Runner that execute them. Keeping the
// Manager here rather than per turn is what lets a later turn's task_output
// find a task an earlier turn launched, and lets a background child outlive
// the turn that spawned it.
type TaskRegistry struct {
	mu        sync.Mutex
	tasks     map[string]*TaskRecord
	order     []string
	turnID    string
	closed    bool
	nextSeq   int
	recordEvt func(sessions.Event, bool)
	space     *taskSpace
}

// taskSpace is the session-scoped eino execution substrate behind the
// registry; built lazily on the first delegating turn and reused after.
type taskSpace struct {
	store   *BridgingTaskStore
	manager *bgtask.Manager
	runner  *backgroundlocal.Runner
}

func NewTaskRegistry(turnID string, recordEvt func(sessions.Event, bool)) *TaskRegistry {
	return &TaskRegistry{
		tasks:     make(map[string]*TaskRecord),
		turnID:    turnID,
		recordEvt: recordEvt,
	}
}

func (r *TaskRegistry) SetRecord(record func(sessions.Event, bool)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordEvt = record
}

func (r *TaskRegistry) SetTurnID(turnID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnID = turnID
	if r.space != nil {
		r.space.store.SetTurnID(turnID)
	}
}

// Space returns the session's background-task Manager and local Runner,
// building them on first use. outputBase is the host directory the
// transcripts' workspace-relative paths resolve against; turnID is stamped on
// tasks created from now on.
func (r *TaskRegistry) Space(ctx context.Context, outputBase, turnID string) (*bgtask.Manager, *backgroundlocal.Runner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.space != nil {
		r.space.store.SetTurnID(turnID)
		return r.space.manager, r.space.runner, nil
	}

	store := NewBridgingTaskStore(bgtask.NewInMemoryStore(nil), r, turnID, outputBase)
	executors := bgtask.NewExecutorRegistry()
	mgr, err := bgtask.New(ctx, &bgtask.Config{
		Tasks:      store,
		TaskEvents: store,
		Executors:  executors,
		IDGen: func(context.Context, *bgtask.AllocateTaskIDRequest) (string, error) {
			return r.AllocateTask(), nil
		},
		SendTaskCreatedEvent: func(context.Context, *bgtask.Task) error {
			return nil
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build background task manager: %w", err)
	}
	runner, err := backgroundlocal.New(&backgroundlocal.Config{
		Manager:   mgr,
		Executors: executors,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build background local runner: %w", err)
	}
	r.space = &taskSpace{store: store, manager: mgr, runner: runner}
	return mgr, runner, nil
}

// AllocateTask reserves the next task id of the session ("task-1", ...).
func (r *TaskRegistry) AllocateTask() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	id := fmt.Sprintf("task-%d", r.nextSeq)
	r.tasks[id] = &TaskRecord{
		ID:     id,
		Status: TaskStatusRunning,
		TurnID: r.turnID,
		Done:   make(chan struct{}),
	}
	r.order = append(r.order, id)
	return id
}

// RegisterTask records a newly created task from BridgingTaskStore.Create:
// its description, its transcript file (workspace-relative, resolved against
// outputBase for the registry's own writes) and its lane.
func (r *TaskRegistry) RegisterTask(id, description, transcriptPath, outputBase, turnID string, background bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tasks[id]
	if !ok {
		rec = &TaskRecord{
			ID:     id,
			TurnID: turnID,
			Done:   make(chan struct{}),
		}
		r.tasks[id] = rec
		r.order = append(r.order, id)
	}
	rec.Description = description
	rec.Status = TaskStatusRunning
	rec.Background = background
	if transcriptPath != "" {
		rec.TranscriptPath = transcriptPath
		if outputBase != "" {
			rec.transcriptAbs = filepath.Join(outputBase, transcriptPath)
		}
	}
}

func (r *TaskRegistry) Lookup(id string) (*TaskRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return rec, nil
}

// Progress folds one appended transcript chunk (JSONL records as written by
// the transcript formatter) into the running record and re-broadcasts its
// live snapshot, rate-limited per task.
func (r *TaskRegistry) Progress(id string, data []byte) {
	summary, count := summarizeTranscriptChunk(data)
	if count == 0 {
		return
	}
	r.mu.Lock()
	rec, ok := r.tasks[id]
	if !ok || rec.Terminal() {
		r.mu.Unlock()
		return
	}
	rec.Steps += count
	if summary != "" {
		rec.Progress = summary
	}
	now := time.Now()
	if !rec.lastProgressEmit.IsZero() && now.Sub(rec.lastProgressEmit) < progressEmitInterval {
		r.mu.Unlock()
		return
	}
	rec.lastProgressEmit = now
	r.mu.Unlock()
	r.EmitTask(id)
}

// summarizeTranscriptChunk reads the chunk's JSONL records and returns a
// one-line summary of the most telling one plus the record count. Tool calls
// and text beat reasoning; the task header and the terminal result are not
// progress.
func summarizeTranscriptChunk(data []byte) (string, int) {
	summary := ""
	reasoning := false
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		count++
		switch entry.Step {
		case StepTool:
			if strings.HasPrefix(entry.Content, "call ") {
				summary = progressLine(entry.Content)
			}
		case StepText:
			summary = progressLine(entry.Content)
		case StepReasoning:
			reasoning = true
		}
	}
	if summary == "" && reasoning {
		summary = "Thinking…"
	}
	return summary, count
}

// progressLine collapses content to its first non-empty line, bounded.
func progressLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > progressLineLimit {
			cut := line[:progressLineLimit]
			if idx := strings.LastIndex(cut, " "); idx > progressLineLimit/2 {
				cut = cut[:idx]
			}
			return cut + "…"
		}
		return line
	}
	return ""
}

// appendTranscript writes one record to the task's transcript file, creating
// it if the child has not opened it yet. Best effort: the transcript is a
// convenience copy, never the record of truth.
func (r *TaskRegistry) appendTranscript(id string, entry TranscriptEntry) {
	r.mu.Lock()
	rec, ok := r.tasks[id]
	path := ""
	if ok {
		path = rec.transcriptAbs
	}
	r.mu.Unlock()
	if path == "" {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

func (r *TaskRegistry) Finish(id string, status sessions.ToolStatus, result string) {
	r.mu.Lock()
	rec, ok := r.tasks[id]
	if !ok || rec.Terminal() {
		r.mu.Unlock()
		return
	}
	rec.Status = status
	rec.Result = result
	rec.Cancel = nil
	close(rec.Done)
	r.mu.Unlock()
	r.appendTranscript(id, TranscriptEntry{Step: StepResult, Status: string(status), Content: result})
	r.EmitTask(id)
}

func (r *TaskRegistry) EmitTask(id string) {
	r.mu.Lock()
	rec, ok := r.tasks[id]
	if !ok || r.recordEvt == nil {
		r.mu.Unlock()
		return
	}
	snap := rec.Snapshot()
	turnID := rec.TurnID
	if turnID == "" {
		turnID = r.turnID
	}
	durable := rec.Terminal()
	recordFn := r.recordEvt
	r.mu.Unlock()

	ev := sessions.Event{
		TurnID: turnID,
		Kind:   sessions.KindTaskActivity,
		Task:   snap,
	}
	recordFn(ev, durable)
}

func (r *TaskRegistry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	var toCancel []context.CancelFunc
	for _, rec := range r.tasks {
		if rec.Status == TaskStatusRunning && rec.Cancel != nil {
			toCancel = append(toCancel, rec.Cancel)
		}
	}
	r.mu.Unlock()

	for _, cancel := range toCancel {
		cancel()
	}
}

// BridgingTaskStore is the eino task store the Manager writes through: every
// lifecycle transition and progress record is mirrored into the session's
// TaskRegistry, which is what the UI observes.
type BridgingTaskStore struct {
	*bgtask.InMemoryStore
	registry   *TaskRegistry
	outputBase string

	mu     sync.Mutex
	turnID string
}

// NewBridgingTaskStore wraps inner; outputBase is the host directory the
// tasks' workspace-relative transcript paths resolve against.
func NewBridgingTaskStore(inner *bgtask.InMemoryStore, registry *TaskRegistry, turnID, outputBase string) *BridgingTaskStore {
	return &BridgingTaskStore{
		InMemoryStore: inner,
		registry:      registry,
		turnID:        turnID,
		outputBase:    outputBase,
	}
}

// SetTurnID changes the turn stamped on tasks created from now on.
func (s *BridgingTaskStore) SetTurnID(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnID = turnID
}

func (s *BridgingTaskStore) currentTurnID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnID
}

func (s *BridgingTaskStore) Create(ctx context.Context, req *bgtask.CreateTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil && task != nil {
		// Every delegation runs in the background lane (DelegationArgs forces
		// it), so the record is a background task by construction.
		s.registry.RegisterTask(task.Spec.ID, task.Spec.Description, task.Spec.OutputFile, s.outputBase, s.currentTurnID(), true)
		s.registry.appendTranscript(task.Spec.ID, TranscriptEntry{Step: StepTask, Content: task.Spec.Description})
		s.registry.EmitTask(task.Spec.ID)
	}
	return task, nil
}

// AppendTaskEvent receives the child's streamed transcript records (the
// executor's EmitProgress) and forwards each newly inserted chunk as live
// progress.
func (s *BridgingTaskStore) AppendTaskEvent(ctx context.Context, req *bgtask.AppendTaskEventRequest) (*bgtask.AppendTaskEventResult, error) {
	res, err := s.InMemoryStore.AppendTaskEvent(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil && req != nil && res != nil && res.Inserted {
		s.registry.Progress(req.TaskID, req.Data)
	}
	return res, nil
}

func (s *BridgingTaskStore) Complete(ctx context.Context, req *bgtask.CompleteTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil {
		s.registry.Finish(req.TaskID, TaskStatusCompleted, string(req.Data))
	}
	return task, nil
}

func (s *BridgingTaskStore) Fail(ctx context.Context, req *bgtask.FailTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Fail(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil {
		s.registry.Finish(req.TaskID, TaskStatusFailed, req.Error)
	}
	return task, nil
}

func (s *BridgingTaskStore) AckCancel(ctx context.Context, req *bgtask.AckCancelRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.AckCancel(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil {
		s.registry.Finish(req.TaskID, TaskStatusStopped, "")
	}
	return task, nil
}
