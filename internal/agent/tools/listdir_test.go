package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirTool(t *testing.T) {
	tmpDir := t.TempDir()
	workspace, _ := filepath.Abs(tmpDir)

	scope := &Scope{
		Workspace: workspace,
	}

	toolObj := &listDirTool{}
	invokable := toolObj.Build(scope)

	// Create a file and a dir
	_ = os.WriteFile(filepath.Join(workspace, "file1.txt"), []byte("file 1"), 0644)
	_ = os.Mkdir(filepath.Join(workspace, "subdir"), 0755)

	ctx := context.Background()

	// 1. Success case
	res, err := invokable.InvokableRun(ctx, `{"path": "."}`)
	if err != nil {
		t.Fatalf("list_dir failed: %v", err)
	}

	if !strings.Contains(res, "file1.txt (file,") {
		t.Errorf("list_dir output does not contain file1.txt: %s", res)
	}
	if !strings.Contains(res, "subdir (dir,") {
		t.Errorf("list_dir output does not contain subdir: %s", res)
	}

	// 2. Traversal blocked case
	_, err = invokable.InvokableRun(ctx, `{"path": "../escaped"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
