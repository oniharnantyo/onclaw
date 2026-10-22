package subagents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

const (
	TaskStatusRunning   = sessions.ToolStatusRunning
	TaskStatusCompleted = sessions.ToolStatusCompleted
	TaskStatusFailed    = sessions.ToolStatusFailed
	TaskStatusStopped   = sessions.ToolStatusStopped
)

var ErrTaskNotFound = errors.New("subagents: no such task in this run")
var ErrTaskNotRunning = errors.New("subagents: task is not running")

type TaskRecord struct {
	ID             string
	Description    string
	Status         sessions.ToolStatus
	Result         string
	TranscriptPath string
	Background     bool
	TurnID         string

	Cancel     context.CancelFunc
	Done       chan struct{}
	Transcript *TaskTranscript
}

func (t *TaskRecord) Terminal() bool {
	return t == nil || t.Status != TaskStatusRunning
}

func (t TaskRecord) Snapshot() *sessions.TaskActivity {
	return &sessions.TaskActivity{
		TaskID:         t.ID,
		Description:    TaskSummary(t.Description),
		Status:         t.Status,
		Result:         t.Result,
		TranscriptPath: t.TranscriptPath,
		Background:     t.Background,
	}
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

type TaskRegistry struct {
	mu        sync.Mutex
	tasks     map[string]*TaskRecord
	order     []string
	turnID    string
	closed    bool
	nextSeq   int
	activeTx  *TaskTranscript
	recordEvt func(sessions.Event)
}

func NewTaskRegistry(turnID string, recordEvt func(sessions.Event)) *TaskRegistry {
	return &TaskRegistry{
		tasks:     make(map[string]*TaskRecord),
		turnID:    turnID,
		recordEvt: recordEvt,
	}
}

func (r *TaskRegistry) AllocateTask(tasksDir string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	id := fmt.Sprintf("task-%d", r.nextSeq)
	relPath := ""
	if tasksDir != "" {
		relPath = filepath.Join(workspaces.DirTasks, id+TaskTranscriptSuffix)
	}
	r.tasks[id] = &TaskRecord{
		ID:             id,
		Status:         TaskStatusRunning,
		TranscriptPath: relPath,
		TurnID:         r.turnID,
		Done:           make(chan struct{}),
	}
	r.order = append(r.order, id)
	return id
}

func (r *TaskRegistry) SetActiveTranscript(tx *TaskTranscript) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activeTx = tx
}

func (r *TaskRegistry) ActiveTranscript() *TaskTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeTx
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
	r.mu.Unlock()

	ev := sessions.Event{
		TurnID: turnID,
		Kind:   sessions.KindTaskActivity,
		Task:   snap,
	}
	r.recordEvt(ev)
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

type BridgingTaskStore struct {
	*bgtask.InMemoryStore
	registry *TaskRegistry
	turnID   string
	tasksDir string
}

func NewBridgingTaskStore(inner *bgtask.InMemoryStore, registry *TaskRegistry, turnID, tasksDir string) *BridgingTaskStore {
	return &BridgingTaskStore{
		InMemoryStore: inner,
		registry:      registry,
		turnID:        turnID,
		tasksDir:      tasksDir,
	}
}

func (s *BridgingTaskStore) Create(ctx context.Context, req *bgtask.CreateTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.registry != nil && task != nil {
		s.registry.mu.Lock()
		if rec, ok := s.registry.tasks[task.Spec.ID]; ok {
			rec.Background = true
		}
		s.registry.mu.Unlock()
	}
	return task, nil
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
