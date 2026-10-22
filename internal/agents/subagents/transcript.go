package subagents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// TasksOutputDir is the workspace-relative directory child transcripts land in.
const TasksOutputDir = workspaces.DirTasks

// TaskTranscriptSuffix is the transcript file's extension within .tasks/.
const TaskTranscriptSuffix = ".md"

// TranscriptStep names one kind of line in a materialized transcript.
type TranscriptStep string

const (
	StepTask      TranscriptStep = "task"
	StepText      TranscriptStep = "text"
	StepReasoning TranscriptStep = "reasoning"
	StepTool      TranscriptStep = "tool"
	StepResult    TranscriptStep = "result"
)

// TranscriptEntry is one JSONL record of a materialized transcript.
type TranscriptEntry struct {
	Step    TranscriptStep `json:"step"`
	Content string         `json:"content,omitempty"`
	Status  string         `json:"status,omitempty"`
}

// TaskTranscript writes one delegated task's transcript file.
type TaskTranscript struct {
	path string
	file *os.File

	mu     sync.Mutex
	closed bool
}

// NewTaskTranscript opens (creating) the transcript for one task under tasksDir.
func NewTaskTranscript(tasksDir, taskID, description string) (*TaskTranscript, error) {
	if err := os.MkdirAll(tasksDir, 0o700); err != nil {
		return nil, fmt.Errorf("create tasks directory: %w", err)
	}
	path := filepath.Join(tasksDir, taskID+TaskTranscriptSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create task transcript: %w", err)
	}
	t := &TaskTranscript{path: path, file: file}
	if err := t.Append(TranscriptEntry{Step: StepTask, Content: description}); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

// Append writes one transcript record.
func (t *TaskTranscript) Append(entry TranscriptEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode transcript entry: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	if _, err := t.file.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write transcript entry: %w", err)
	}
	return nil
}

// Close flushes and closes the transcript file.
func (t *TaskTranscript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.file.Close()
}
