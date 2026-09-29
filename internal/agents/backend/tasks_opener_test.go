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
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

// TestTasksAppendOpener_HappyPath covers the lane call shape end to end: the
// requested path arrives OutputDir-joined (".tasks/<name>.output", exactly
// what the subagent and fs-shell lanes hand over), opens append-only, and
// content accumulates across reopen.
func TestTasksAppendOpener_HappyPath(t *testing.T) {
	ctx := context.Background()
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	requested := TasksOutputDir + "/task-1.output"
	write, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(requested))
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if _, err := io.WriteString(write, "line one\n"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Reopen the same path: content accumulates instead of truncating.
	reopen, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(requested))
	if err != nil {
		t.Fatalf("reopen append: %v", err)
	}
	if _, err := reopen.Write([]byte("line two\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if err := reopen.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(agentDir, TasksOutputDir, "task-1.output"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got, want := string(content), "line one\nline two\n"; got != want {
		t.Fatalf("accumulated content = %q, want %q", got, want)
	}
}

// TestTasksAppendOpener_EscapeAttempts pins the jail: traversal and absolute
// paths never open, and nothing lands outside .tasks.
func TestTasksAppendOpener_EscapeAttempts(t *testing.T) {
	ctx := context.Background()
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	for _, requested := range []string{
		"../escape.output",
		"tasks/../../escape.output",
		"/etc/passwd",
		filepath.Join(agentDir, TasksOutputDir, "abs.output"),
		"",
		TasksOutputDir,
	} {
		w, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(requested))
		if err == nil {
			w.Close()
			t.Fatalf("OpenAppend(%q) succeeded, want rejection", requested)
		}
	}
	// The escape targets must not exist: only .tasks ever gets created.
	if _, err := os.Stat(filepath.Dir(agentDir)); err != nil {
		t.Fatalf("stat agent dir parent: %v", err)
	}
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		t.Fatalf("read agent dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("agent dir mutated during rejections: %v entries", len(entries))
	}
}

// TestTasksAppendOpener_MissingDirCreated: the .tasks directory is created on
// open when absent (fresh agent dirs ship without it), with 0o755.
func TestTasksAppendOpener_MissingDirCreated(t *testing.T) {
	ctx := context.Background()
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	w, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(TasksOutputDir+"/first.output"))
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	info, err := os.Stat(filepath.Join(agentDir, TasksOutputDir))
	if err != nil {
		t.Fatalf("stat tasks dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("tasks dir is not a directory")
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("tasks dir perms = %o, want 755", got)
	}
	if _, err := os.Stat(filepath.Join(agentDir, TasksOutputDir, "first.output")); err != nil {
		t.Fatalf("output file not created: %v", err)
	}
}

// TestTasksAppendOpener_Subdirectories: nested paths under .tasks resolve
// (mkdir-all on open) — still inside the tasks root.
func TestTasksAppendOpener_Subdirectories(t *testing.T) {
	ctx := context.Background()
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	w, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(TasksOutputDir+"/batch/run.output"))
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if _, err := io.WriteString(w, "nested\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(agentDir, TasksOutputDir, "batch", "run.output"))
	if err != nil {
		t.Fatalf("read nested output: %v", err)
	}
	if got := string(content); got != "nested\n" {
		t.Fatalf("nested content = %q, want %q", got, "nested\n")
	}
}

// TestTasksAppendOpener_CloseIdempotentWriteAfterClose: Close is idempotent
// and a Write after Close is rejected without modifying the file.
func TestTasksAppendOpener_CloseIdempotentWriteAfterClose(t *testing.T) {
	ctx := context.Background()
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	w, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(TasksOutputDir+"/closed.output"))
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if _, err := io.WriteString(w, "kept\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close (idempotent): %v", err)
	}
	if _, err := w.Write([]byte("dropped\n")); err == nil {
		t.Fatal("write after close succeeded, want rejection")
	}

	content, err := os.ReadFile(filepath.Join(agentDir, TasksOutputDir, "closed.output"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if got := string(content); got != "kept\n" {
		t.Fatalf("content = %q, want %q", got, "kept\n")
	}
}

// TestTasksAppendOpener_ContextCancelReleasesHandle: the AppendOpener
// contract bounds the session by ctx — an abandoned handle must not leak the
// fd past ctx cancellation.
func TestTasksAppendOpener_ContextCancelReleasesHandle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	agentDir := t.TempDir()

	opener, err := NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("NewTasksAppendOpener: %v", err)
	}

	w, err := opener.OpenAppend(ctx, einofsOpenAppendRequest(TasksOutputDir+"/abandoned.output"))
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := w.Write([]byte("x")); err != nil {
			if !errors.Is(err, os.ErrClosed) {
				t.Fatalf("write after cancel: %v (want os.ErrClosed)", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handle still writable 2s after ctx cancellation")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// einofsOpenAppendRequest builds the request struct the AppendOpener
// contract takes, keeping the table tests terse.
func einofsOpenAppendRequest(path string) *einofs.OpenAppendRequest {
	return &einofs.OpenAppendRequest{FilePath: path}
}
