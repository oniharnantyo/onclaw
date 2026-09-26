package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newDeleteToolForDir(t *testing.T) (*deleteFileTool, string) {
	t.Helper()
	dir := t.TempDir()
	tool, err := NewDeleteFile(dir)
	if err != nil {
		t.Fatalf("NewDeleteFile: %v", err)
	}
	return tool.(*deleteFileTool), dir
}

func TestDeleteFile_Success(t *testing.T) {
	tool, dir := newDeleteToolForDir(t)
	if err := os.WriteFile(filepath.Join(dir, "BOOTSTRAP.md"), []byte("# Bootstrap"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	out, err := tool.InvokableRun(context.Background(), `{"path":"/workspace/BOOTSTRAP.md"}`)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Deleted /workspace/BOOTSTRAP.md") {
		t.Errorf("expected confirmation naming the mount path, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "BOOTSTRAP.md")); !os.IsNotExist(err) {
		t.Errorf("file should be gone, stat err: %v", err)
	}
}

func TestDeleteFile_RelativePathResolvesInsideJail(t *testing.T) {
	tool, dir := newDeleteToolForDir(t)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"path":"notes.txt"}`); err != nil {
		t.Fatalf("delete relative: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("file should be gone, stat err: %v", err)
	}
}

func TestDeleteFile_MissingFileIsError(t *testing.T) {
	tool, _ := newDeleteToolForDir(t)
	_, err := tool.InvokableRun(context.Background(), `{"path":"/workspace/absent.md"}`)
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing-file error, got %v", err)
	}
}

func TestDeleteFile_RejectsDotDotEscape(t *testing.T) {
	tool, dir := newDeleteToolForDir(t)
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	for _, path := range []string{"/workspace/../outside.txt", "../outside.txt"} {
		_, err := tool.InvokableRun(context.Background(), `{"path":`+quoteJSON(path)+`}`)
		if err == nil || !strings.Contains(err.Error(), "path escape attempt") {
			t.Errorf("%q: expected escape rejection, got %v", path, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside file must survive: %v", err)
	}
}

func TestDeleteFile_RejectsHostAbsolutePaths(t *testing.T) {
	tool, _ := newDeleteToolForDir(t)
	outside, err := os.MkdirTemp("", "onclaw-delete-escape")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	target := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err = tool.InvokableRun(context.Background(), `{"path":`+quoteJSON(target)+`}`)
	if err == nil || !strings.Contains(err.Error(), "absolute paths are not allowed") {
		t.Fatalf("expected host-absolute rejection, got %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("outside file must survive: %v", statErr)
	}
}

func TestDeleteFile_RejectsSymlinkEscape(t *testing.T) {
	tool, dir := newDeleteToolForDir(t)
	outside, err := os.MkdirTemp("", "onclaw-delete-symlink")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	target := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(dir, "escape.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err = tool.InvokableRun(context.Background(), `{"path":"/workspace/escape.md"}`)
	if err == nil || !strings.Contains(err.Error(), "escapes jail") {
		t.Fatalf("expected symlink-escape rejection, got %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("symlink target must survive: %v", statErr)
	}
}

func TestDeleteFile_RejectsWorkspaceRoot(t *testing.T) {
	tool, _ := newDeleteToolForDir(t)
	_, err := tool.InvokableRun(context.Background(), `{"path":"/workspace"}`)
	if err == nil || !strings.Contains(err.Error(), "cannot delete the workspace root") {
		t.Fatalf("expected root rejection, got %v", err)
	}
}

func TestDeleteFile_EmptyPathFails(t *testing.T) {
	tool, _ := newDeleteToolForDir(t)
	if _, err := tool.InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("expected empty-path error, got nil")
	}
}

func TestNewDeleteFile_Construction(t *testing.T) {
	if _, err := NewDeleteFile(""); err == nil {
		t.Error("expected empty-dir construction error")
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := NewDeleteFile(missing); err != nil {
		t.Errorf("missing dir should self-heal at construction, got %v", err)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Errorf("missing dir should be created at construction, stat err: %v", err)
	}
	file := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewDeleteFile(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected not-a-directory construction error, got %v", err)
	}
}

// quoteJSON renders a Go string as a JSON string literal for inline args.
func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
