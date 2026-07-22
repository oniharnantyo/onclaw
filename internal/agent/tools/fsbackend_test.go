package tools_test

import (
	"context"
	"errors"
	"fmt"
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

// TestFSBackendGrepTruncated verifies GrepRaw caps matched content at
// fsGrepCapBytes: a file with far more matched content than the cap yields a
// bounded result set plus a synthetic "truncated" marker, while the kept
// (non-marker) matches are still returned.
func TestFSBackendGrepTruncated(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws

	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("needle abcdefghijklmnopqrstuvwxyz0123456789\n")
	}
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "big.txt", Content: sb.String()}); err != nil {
		t.Fatalf("write: %v", err)
	}

	matches, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "needle", Path: ws})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}

	var marker bool
	var nonMarker int
	var totalBytes int
	for _, m := range matches {
		if strings.Contains(m.Content, "truncated") {
			marker = true
			continue
		}
		nonMarker++
		totalBytes += len(m.Content)
	}

	if !marker {
		t.Errorf("expected a truncation marker in grep results")
	}
	if nonMarker == 0 {
		t.Error("expected non-marker matches to still be present")
	}
	if len(matches) >= 2000 {
		t.Errorf("expected grep results truncated below 2000 matches, got %d", len(matches))
	}
	if totalBytes > 32*1024 {
		t.Errorf("matched content bytes not bounded: %d (cap %d)", totalBytes, 32*1024)
	}
}

// TestFSBackendGrepBounded verifies GrepRaw returns exactly the matches for a
// small file with no truncation marker.
func TestFSBackendGrepBounded(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws

	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "one.txt", Content: "needle here\nsecond line\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	matches, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "needle", Path: ws})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match, got %d: %v", len(matches), matches)
	}
	if strings.Contains(matches[0].Content, "truncated") {
		t.Errorf("did not expect a truncation marker for a small file")
	}
}

// TestFSBackendGlobTruncated verifies GlobInfo caps returned entries at
// fsGlobCapEntries: more than that many files yields a bounded result set plus
// a synthetic "truncated" marker.
func TestFSBackendGlobTruncated(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws

	dir := filepath.Join(ws, "many")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < 250; i++ {
		name := fmt.Sprintf("f%03d.go", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package x\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	infos, err := b.GlobInfo(ctx, &filesystem.GlobInfoRequest{Pattern: "**", Path: ws})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	var marker bool
	for _, info := range infos {
		if strings.Contains(info.Path, "truncated") {
			marker = true
		}
	}
	if !marker {
		t.Errorf("expected a truncation marker in glob results; got %d entries", len(infos))
	}
}

// TestFSBackendGlobBounded verifies GlobInfo returns exactly the files for a
// small set with no truncation marker.
func TestFSBackendGlobBounded(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()
	_ = ws

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("f%d.txt", i)
		if err := os.WriteFile(filepath.Join(ws, name), []byte("x\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	infos, err := b.GlobInfo(ctx, &filesystem.GlobInfoRequest{Pattern: "**", Path: ws})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, info := range infos {
		if strings.Contains(info.Path, "truncated") {
			t.Errorf("did not expect a truncation marker for few files; got %q", info.Path)
		}
	}
	if len(infos) != 3 {
		t.Errorf("expected exactly 3 files, got %d: %v", len(infos), infos)
	}
}

// TestFSBackendSentinelWrapping verifies a sentinel survives Eino-style %w
// wrapping (the Eino invokable_func wraps endpoint errors) and still matches
// errors.Is, which the FSErrorMiddleware relies on.
func TestFSBackendSentinelWrapping(t *testing.T) {
	wrapped := fmt.Errorf("invokable func: %w", tools.ErrFileNotFound)
	if !errors.Is(wrapped, tools.ErrFileNotFound) {
		t.Error("expected errors.Is to match through Eino-style %w wrapping")
	}
	wrappedCtx := fmt.Errorf("inner: %w", tools.ErrEditNotUnique)
	if !errors.Is(wrappedCtx, tools.ErrEditNotUnique) {
		t.Error("expected errors.Is to match nested sentinel")
	}
}

// TestFSBackendSentinelsMatch verifies each expected condition returns the
// correct classified sentinel so the middleware can convert it to an
// observation. Genuine infrastructure failures must NOT match.
func TestFSBackendSentinelsMatch(t *testing.T) {
	b, ws := newTestBackend(t)
	ctx := context.Background()

	// Path traversal -> ErrPathOutsideWorkspace
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "../../etc/passwd", Content: "x"}); err == nil || !errors.Is(err, tools.ErrPathOutsideWorkspace) {
		t.Errorf("expected ErrPathOutsideWorkspace for traversal, got %v", err)
	}

	// Not found -> ErrFileNotFound
	if _, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: "nope.txt"}); err == nil || !errors.Is(err, tools.ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got %v", err)
	}

	// Edit missing -> ErrEditOldStringMissing
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "e.txt", Content: "a"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "e.txt", OldString: "zzz", NewString: "b"}); err == nil || !errors.Is(err, tools.ErrEditOldStringMissing) {
		t.Errorf("expected ErrEditOldStringMissing, got %v", err)
	}
	// Edit non-unique -> ErrEditNotUnique
	if err := b.Write(ctx, &filesystem.WriteRequest{FilePath: "u.txt", Content: "x\nx\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := b.Edit(ctx, &filesystem.EditRequest{FilePath: "u.txt", OldString: "x", NewString: "y"}); err == nil || !errors.Is(err, tools.ErrEditNotUnique) {
		t.Errorf("expected ErrEditNotUnique, got %v", err)
	}

	// Grep empty pattern -> ErrEmptyPattern
	if _, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "", Path: ws}); err == nil || !errors.Is(err, tools.ErrEmptyPattern) {
		t.Errorf("expected ErrEmptyPattern, got %v", err)
	}
	// Grep invalid regex -> ErrInvalidRegex
	if _, err := b.GrepRaw(ctx, &filesystem.GrepRequest{Pattern: "[", Path: ws}); err == nil || !errors.Is(err, tools.ErrInvalidRegex) {
		t.Errorf("expected ErrInvalidRegex, got %v", err)
	}
	// Glob invalid -> ErrInvalidGlob
	if _, err := b.GlobInfo(ctx, &filesystem.GlobInfoRequest{Pattern: "[", Path: ws}); err == nil || !errors.Is(err, tools.ErrInvalidGlob) {
		t.Errorf("expected ErrInvalidGlob, got %v", err)
	}
}
