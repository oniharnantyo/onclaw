package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	workspace, _ := filepath.Abs(tmpDir)

	scope := &Scope{
		Workspace: workspace,
	}

	toolObj := &readFileTool{}
	invokable := toolObj.Build(scope)

	// Write a file to read
	testFile := "hello.txt"
	testContent := "Hello, World!"
	err := os.WriteFile(filepath.Join(workspace, testFile), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()

	// 1. Success case
	res, err := invokable.InvokableRun(ctx, `{"path": "hello.txt"}`)
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if res != testContent {
		t.Errorf("read_file returned %q, expected %q", res, testContent)
	}

	// 2. Traversal blocked case
	_, err = invokable.InvokableRun(ctx, `{"path": "../escaped.txt"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
