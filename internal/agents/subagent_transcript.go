package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// Child transcripts (spec agent-subagents "Child transcripts"): every
// delegated run materializes one file per task under the parent workspace's
// .tasks/ directory, written as the child turn runs. The file lives inside
// the jailed workspace, so the parent's own read_file/ls/grep tools can
// inspect a finished delegation through the jail's mount point
// (/workspace/.tasks/task-N.md).

// tasksOutputDir is the workspace-relative directory child transcripts land
// in: the same reserved directory the workspaces manager creates on demand
// (workspaces.DirTasks).
const tasksOutputDir = workspaces.DirTasks

// taskTranscriptSuffix is the transcript file's extension within .tasks/.
const taskTranscriptSuffix = ".md"

// transcriptStep names one kind of line in a materialized transcript.
type transcriptStep string

const (
	// stepTask is the delegation's own header line: what was asked.
	stepTask transcriptStep = "task"
	// stepText is a slice of the child's assistant output.
	stepText transcriptStep = "text"
	// stepReasoning is a slice of the child's reasoning output.
	stepReasoning transcriptStep = "reasoning"
	// stepTool is one child tool call announcement or result.
	stepTool transcriptStep = "tool"
	// stepResult is the terminal line: the child's outcome and answer.
	stepResult transcriptStep = "result"
)

// transcriptEntry is one JSONL record of a materialized transcript. The
// format is line-oriented on purpose: the file is written while the child
// runs, so an interrupted task still leaves everything produced up to that
// point readable.
type transcriptEntry struct {
	Step    transcriptStep `json:"step"`
	Content string         `json:"content,omitempty"`
	// Status carries the terminal entry's outcome (completed/failed/stopped).
	Status string `json:"status,omitempty"`
}

// taskTranscript writes one delegated task's transcript file. It is safe for
// concurrent use: the child's event drain and the terminal stamp may write
// from different goroutines.
type taskTranscript struct {
	path string
	file *os.File

	mu     sync.Mutex
	closed bool
}

// newTaskTranscript opens (creating) the transcript for one task under the
// parent's workspace .tasks directory. The returned transcript's path is
// workspace-relative (".tasks/task-1.md"); write failures surface to the
// caller, which degrades to running the child without a transcript rather
// than failing the delegation.
func newTaskTranscript(tasksDir, taskID, description string) (*taskTranscript, error) {
	if err := os.MkdirAll(tasksDir, 0o700); err != nil {
		return nil, fmt.Errorf("create tasks directory: %w", err)
	}
	path := filepath.Join(tasksDir, taskID+taskTranscriptSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create task transcript: %w", err)
	}
	t := &taskTranscript{path: path, file: file}
	// The task line is the file's first record so a reader knows what was
	// asked before reading any output.
	if err := t.append(transcriptEntry{Step: stepTask, Content: description}); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

// append writes one transcript record. Writes are line-buffered by the OS
// (unbuffered file writes), so a concurrent reader sees every appended line
// immediately — the same visibility contract the donor's task output store
// relies on.
func (t *taskTranscript) append(entry transcriptEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode transcript entry: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("write transcript %s: file is closed", t.path)
	}
	if _, err := t.file.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write transcript %s: %w", t.path, err)
	}
	return nil
}

// text appends a slice of assistant output, skipping empty slices.
func (t *taskTranscript) text(content string) {
	if content == "" {
		return
	}
	_ = t.append(transcriptEntry{Step: stepText, Content: content})
}

// reasoning appends a slice of reasoning output, skipping empty slices.
func (t *taskTranscript) reasoning(content string) {
	if content == "" {
		return
	}
	_ = t.append(transcriptEntry{Step: stepReasoning, Content: content})
}

// tool appends one tool-call record: the announcement carries the call's
// name, the terminal record its result.
func (t *taskTranscript) tool(content string) {
	if content == "" {
		return
	}
	_ = t.append(transcriptEntry{Step: stepTool, Content: content})
}

// terminal stamps the task's outcome as the transcript's final line, then
// closes the file. The stamp is best-effort: a failed terminal write must not
// change the task's reported outcome.
func (t *taskTranscript) terminal(status, result string) {
	_ = t.append(transcriptEntry{Step: stepResult, Status: status, Content: result})
	_ = t.Close()
}

// Close closes the underlying file. Idempotent.
func (t *taskTranscript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.file.Close()
}

// jailPath renders the transcript's path inside the jailed mount, which is
// how the parent's file tools address it ("/workspace/.tasks/task-1.md").
func jailPath(mount, workspaceRelative string) string {
	return strings.TrimSuffix(mount, "/") + "/" + strings.TrimPrefix(workspaceRelative, "/")
}
