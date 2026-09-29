/*
 * Copyright 2025 OnClaw Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the the License for the specific language governing permissions and
 * limitations under the License.
 */

package backend

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

// TasksOutputDir is the canonical agent-dir-relative directory every
// background task output file lands in (add-agent-subagents-background
// D6/D11). The subagent and fs-shell background lanes join it into the paths
// they hand the append opener, the opener refuses anything that would land
// outside it, and the jail's read_file can therefore read completed output
// back. background.go re-exports the same name so the runner wires one
// canonical constant everywhere.
const TasksOutputDir = ".tasks"

// tasksAppendOpener is the jailed AppendOpener for background task output
// files: append-only opens resolved under <agentDir>/.tasks, with traversal
// and absolute-path escapes rejected (design D6).
type tasksAppendOpener struct {
	root string // resolved <agentDir>/.tasks
}

// Compile-time proof the opener satisfies the optional Backend extension the
// background lanes require.
var _ einofs.AppendOpener = (*tasksAppendOpener)(nil)

// NewTasksAppendOpener builds the AppendOpener the background task lanes
// write task transcripts through (design D6). agentDir is the agent's jail
// root and must exist; the .tasks directory beneath it is created lazily on
// the first open. Requested paths resolve under <agentDir>/.tasks ONLY —
// both lane callers pass OutputDir-joined paths (".tasks/<id>.output", the
// TasksOutputDir prefix), and a leading TasksOutputDir segment is stripped
// before joining so the requested file is the one opened.
func NewTasksAppendOpener(agentDir string) (einofs.AppendOpener, error) {
	if agentDir == "" {
		return nil, fmt.Errorf("tasks append opener: agent dir cannot be empty")
	}
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("tasks append opener: resolve agent dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("tasks append opener: resolve agent dir %s: %w", agentDir, err)
	}
	return &tasksAppendOpener{root: filepath.Join(resolved, TasksOutputDir)}, nil
}

// OpenAppend opens an append stream to the requested task output file,
// creating it (and its .tasks home) if absent.
func (o *tasksAppendOpener) OpenAppend(ctx context.Context, req *einofs.OpenAppendRequest) (io.WriteCloser, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("tasks append opener: %w", ctx.Err())
	}
	target, err := o.resolve(req.FilePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("tasks append opener: create tasks directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tasks append opener: open %s: %w", req.FilePath, err)
	}
	w := &tasksAppendWriter{file: file, done: make(chan struct{})}
	// The AppendOpener contract bounds the session by ctx: callers may
	// abandon the handle without a Close (a canceled run), so release the fd
	// on ctx cancellation too — never only in Close.
	go func() {
		select {
		case <-ctx.Done():
			w.Close()
		case <-w.done:
		}
	}()
	return w, nil
}

// resolve validates a requested output path and maps it under the .tasks
// root. Empty paths, any ".." segment, and absolute paths are rejected.
func (o *tasksAppendOpener) resolve(requested string) (string, error) {
	if requested == "" {
		return "", fmt.Errorf("tasks append opener: path cannot be empty")
	}
	if strings.Contains(requested, "..") {
		return "", fmt.Errorf("tasks append opener: path escape attempt: %q contains \"..\"", requested)
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("tasks append opener: absolute paths are not allowed: %q (task output lives under %s)", requested, TasksOutputDir)
	}
	cleaned := filepath.Clean(requested)
	// Lane callers hand over OutputDir-joined paths; strip the canonical
	// prefix when present so the joined file is exactly the one opened.
	if cleaned == TasksOutputDir {
		cleaned = "."
	} else if prefix := TasksOutputDir + string(filepath.Separator); strings.HasPrefix(cleaned, prefix) {
		cleaned = strings.TrimPrefix(cleaned, prefix)
	}
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("tasks append opener: path must name a file: %q", requested)
	}
	return filepath.Join(o.root, cleaned), nil
}

// tasksAppendWriter is the append handle OpenAppend returns. os.File writes
// are unbuffered, so every successful Write is immediately visible to a
// concurrent read (the AppendOpener visibility contract) and a failed write
// leaves the broken fd sticky — the next Write returns its error.
type tasksAppendWriter struct {
	file *os.File
	done chan struct{} // closed on first Close; stops the ctx release watcher
	once sync.Once
}

func (w *tasksAppendWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

// WriteString implements io.StringWriter so text callers (the lanes append
// JSONL lines) skip the []byte copy.
func (w *tasksAppendWriter) WriteString(s string) (int, error) {
	return w.file.WriteString(s)
}

// Close closes the handle. Idempotent; a Write after Close returns the
// os.File closed error without touching the file.
func (w *tasksAppendWriter) Close() error {
	var err error
	w.once.Do(func() {
		err = w.file.Close()
		close(w.done)
	})
	return err
}
