package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func newTestBackend(t *testing.T) (filesystem.Backend, string) {
	t.Helper()
	ws, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	return tools.NewFSBackend(ws), ws
}

func TestFSBackendPathTraversalBlocked(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := context.Background()

	// Read escape
	if _, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: "../escaped.txt"}); err == nil {
		t.Error("expected path traversal blocked on read")
	}
	// Write escape
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "../../etc/passwd", Content: "x"}); err == nil {
		t.Error("expected path traversal blocked on write")
	}
	// Edit escape
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "../x", OldString: "a", NewString: "b"}); err == nil {
		t.Error("expected path traversal blocked on edit")
	}
	// Absolute escape
	if _, err := b.LsInfo(ctx, &filesystem.LsInfoRequest{Path: "/etc"}); err == nil {
		t.Error("expected absolute escape blocked on ls")
	}
}

func TestFSBackendReadOffsetLimit(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := context.Background()
	content := strings.Join([]string{"line1", "line2", "line3", "line4", "line5"}, "\n")
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "f.txt", Content: content}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read all (default limit 2000 covers it)
	fc, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: "f.txt", Offset: 1, Limit: 2000})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if fc.Content != content {
		t.Errorf("full read mismatch:\n got %q\nwant %q", fc.Content, content)
	}

	// Read middle slice: lines 2-3
	fc, err = b.Read(ctx, &filesystem.ReadRequest{FilePath: "f.txt", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("read slice: %v", err)
	}
	if fc.Content != "line2\nline3" {
		t.Errorf("slice read mismatch: got %q want %q", fc.Content, "line2\nline3")
	}
}

func TestFSBackendEditExactMatch(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws
	initial := "line 1\ntarget line\nline 3\ntarget line\nline 5\n"
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "d.txt", Content: initial}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Non-unique rejected
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "d.txt", OldString: "target line", NewString: "replaced"}); err == nil {
		t.Error("expected non-unique edit rejected")
	}
	// Missing rejected
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "d.txt", OldString: "missing", NewString: "replaced"}); err == nil {
		t.Error("expected missing edit rejected")
	}
	// Unique succeeds
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "d.txt", OldString: "line 1", NewString: "first line"}); err != nil {
		t.Fatalf("unique edit failed: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "d.txt"))
	if string(got) != "first line\ntarget line\nline 3\ntarget line\nline 5\n" {
		t.Errorf("edit result unexpected: %q", string(got))
	}
	// ReplaceAll replaces every occurrence
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "d.txt", OldString: "target line", NewString: "X", ReplaceAll: true}); err != nil {
		t.Fatalf("replaceall failed: %v", err)
	}
	got, _ = os.ReadFile(filepath.Join(ws, "d.txt"))
	if strings.Count(string(got), "X") != 2 {
		t.Errorf("replaceall did not replace all: %q", string(got))
	}
}

func TestFSBackendRedaction(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws
	secret := "sk-ABCDEFGHIJKLMNOPQRSTUVW"
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "secret.txt", Content: "key=" + secret + "\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	fc, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: "secret.txt", Offset: 1, Limit: 2000})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(fc.Content, secret) {
		t.Errorf("secret not redacted in read: %q", fc.Content)
	}
	if !strings.Contains(fc.Content, "[REDACTED]") {
		t.Errorf("expected redaction marker in read: %q", fc.Content)
	}

	// grep redaction
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "code.go", Content: "token := \"nvapi-ABCDEFGHIJKLMNOPQRSTUVW\"\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	matches, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "nvapi-", Path: ws})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected a grep match")
	}
	if strings.Contains(matches[0].Content, "nvapi-ABCDEFGHIJKLMNOPQRSTUVW") {
		t.Errorf("secret not redacted in grep: %q", matches[0].Content)
	}
}

func TestFSBackendGlobAndGrep(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws
	os.WriteFile(filepath.Join(ws, "a.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(ws, "b.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(ws, "c.txt"), []byte("hello world\n"), 0644)
	sub := filepath.Join(ws, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "d.go"), []byte("package sub\n"), 0644)

	infos, err := b.GlobInfo(ctx, &filesystem.GlobInfoRequest{Pattern: "**/*.go", Path: ws})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(infos) != 3 {
		t.Errorf("expected 3 .go files, got %d: %v", len(infos), infos)
	}
	// Confinement: every glob result must resolve inside the workspace.
	for _, info := range infos {
		if filepath.IsAbs(info.Path) || strings.Contains(info.Path, "..") {
			t.Errorf("glob returned an unsafe path: %q", info.Path)
		}
		resolved := filepath.Clean(filepath.Join(ws, info.Path))
		rel, err := filepath.Rel(ws, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("glob returned a path escaping the workspace: %q", info.Path)
		}
	}

	matches, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "hello", Path: ws})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(matches) != 1 || matches[0].Content != "hello world" {
		t.Errorf("unexpected grep result: %v", matches)
	}

	// context lines
	matches, err = b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "hello", Path: ws, BeforeLines: 0, AfterLines: 0})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

func TestFSBackendLsInfo(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws
	os.WriteFile(filepath.Join(ws, "file1.txt"), []byte("x"), 0644)
	os.MkdirAll(filepath.Join(ws, "subdir"), 0755)

	infos, err := b.LsInfo(ctx, &filesystem.LsInfoRequest{Path: ws})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	var names []string
	for _, i := range infos {
		names = append(names, i.Path)
	}
	if !contains(names, "file1.txt") || !contains(names, "subdir") {
		t.Errorf("ls missing entries: %v", names)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
