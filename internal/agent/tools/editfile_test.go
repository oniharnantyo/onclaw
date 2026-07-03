package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func TestEditFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	workspace, _ := filepath.Abs(tmpDir)

	scope := &tools.Scope{
		Workspace: workspace,
	}

	toolObj := getTool("edit_file")
	if toolObj == nil {
		t.Fatal("edit_file tool not found in registry")
	}
	invokable := toolObj.Build(scope)

	ctx := context.Background()

	// Setup initial file
	testFile := "document.txt"
	initialContent := "line 1\ntarget line\nline 3\ntarget line\nline 5\n"
	err := os.WriteFile(filepath.Join(workspace, testFile), []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// 1. Non-unique match rejected
	_, err = invokable.InvokableRun(ctx, `{"path": "document.txt", "old_string": "target line", "new_string": "replaced"}`)
	if err == nil {
		t.Error("expected error for non-unique match, got nil")
	}

	// 2. Missing match rejected
	_, err = invokable.InvokableRun(ctx, `{"path": "document.txt", "old_string": "missing line", "new_string": "replaced"}`)
	if err == nil {
		t.Error("expected error for missing match, got nil")
	}

	// 3. Unique match succeeds
	res, err := invokable.InvokableRun(ctx, `{"path": "document.txt", "old_string": "line 1", "new_string": "first line"}`)
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}
	if res != "File edited successfully." {
		t.Errorf("expected success message, got: %s", res)
	}

	// Verify file content after unique replacement
	data, err := os.ReadFile(filepath.Join(workspace, testFile))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expectedContent := "first line\ntarget line\nline 3\ntarget line\nline 5\n"
	if string(data) != expectedContent {
		t.Errorf("expected content:\n%s\ngot:\n%s", expectedContent, string(data))
	}

	// 4. Traversal blocked case
	_, err = invokable.InvokableRun(ctx, `{"path": "../escaped.txt", "old_string": "something", "new_string": "replaced"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
