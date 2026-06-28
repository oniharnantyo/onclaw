package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	workspace, _ := filepath.Abs(tmpDir)

	scope := &Scope{
		Workspace: workspace,
	}

	toolObj := &writeFileTool{}
	invokable := toolObj.Build(scope)

	ctx := context.Background()

	// 1. Success case
	testFile := "newfile.txt"
	testContent := "New Content"
	res, err := invokable.InvokableRun(ctx, `{"path": "newfile.txt", "content": "New Content"}`)
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	if res != "File written successfully." {
		t.Errorf("expected success message, got: %s", res)
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(workspace, testFile))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != testContent {
		t.Errorf("written content was %q, expected %q", string(data), testContent)
	}

	// 2. Traversal blocked case
	_, err = invokable.InvokableRun(ctx, `{"path": "../escaped.txt", "content": "should fail"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
