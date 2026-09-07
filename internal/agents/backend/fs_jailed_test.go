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
	"os"
	"path/filepath"
	"strings"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

func TestFilesystemJail_EscapeAttempts(t *testing.T) {
	ctx := context.Background()

	t.Run("path escape with ..", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Try to read a file outside the jail using ..
		_, err = jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "../../../etc/passwd",
		})
		if err == nil {
			t.Error("expected error for path with .., got nil")
		}
		if !strings.Contains(err.Error(), "..") {
			t.Errorf("expected error mentioning '..', got: %v", err)
		}
	})

	t.Run("absolute path outside jail", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Try to read using absolute path
		_, err = jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "/etc/passwd",
		})
		if err == nil {
			t.Error("expected error for absolute path, got nil")
		}
		if !strings.Contains(err.Error(), "absolute") {
			t.Errorf("expected error mentioning 'absolute', got: %v", err)
		}
	})

	t.Run("symlink to outside file", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Create a file outside the jail
		outsideDir := t.TempDir()
		outsideFile := filepath.Join(outsideDir, "outside.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
			t.Fatalf("create outside file: %v", err)
		}

		// Create a symlink inside the jail pointing outside
		symlinkPath := filepath.Join(root, "escape_link")
		if err := os.Symlink(outsideFile, symlinkPath); err != nil {
			t.Fatalf("create symlink: %v", err)
		}

		// Try to read through the symlink
		_, err = jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "escape_link",
		})
		if err == nil {
			t.Error("expected error for symlink escaping jail, got nil")
		}
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("expected error mentioning 'escapes', got: %v", err)
		}
	})

	t.Run("symlink to outside directory", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Create a directory outside the jail
		outsideDir := t.TempDir()

		// Create a symlink inside the jail pointing to outside directory
		symlinkPath := filepath.Join(root, "escape_dir")
		if err := os.Symlink(outsideDir, symlinkPath); err != nil {
			t.Fatalf("create directory symlink: %v", err)
		}

		// Try to list through the symlink directory
		_, err = jail.LsInfo(ctx, &einofs.LsInfoRequest{
			Path: "escape_dir",
		})
		if err == nil {
			t.Error("expected error for directory symlink escaping jail, got nil")
		}
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("expected error mentioning 'escapes', got: %v", err)
		}
	})
}

func TestFilesystemJail_InJailOperations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	jail, err := NewFilesystemJail(root)
	if err != nil {
		t.Fatalf("create jail: %v", err)
	}

	t.Run("write and read file", func(t *testing.T) {
		// Write a file
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "test.txt",
			Content:  "Hello, World!",
		})
		if err != nil {
			t.Fatalf("write file: %v", err)
		}

		// Read it back
		content, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "test.txt",
		})
		if err != nil {
			t.Fatalf("read file: %v", err)
		}

		if content.Content != "Hello, World!" {
			t.Errorf("expected 'Hello, World!', got %q", content.Content)
		}
	})

	t.Run("write to nested path", func(t *testing.T) {
		// Write to a nested directory (should be created automatically)
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "subdir/nested/file.txt",
			Content:  "nested content",
		})
		if err != nil {
			t.Fatalf("write to nested path: %v", err)
		}

		// Read it back
		content, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "subdir/nested/file.txt",
		})
		if err != nil {
			t.Fatalf("read nested file: %v", err)
		}

		if content.Content != "nested content" {
			t.Errorf("expected 'nested content', got %q", content.Content)
		}
	})

	t.Run("list directory", func(t *testing.T) {
		// Create some files
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "dir/file1.txt",
			Content:  "content1",
		})
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "dir/file2.txt",
			Content:  "content2",
		})

		// List directory
		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{
			Path: "dir",
		})
		if err != nil {
			t.Fatalf("list directory: %v", err)
		}

		if len(infos) != 2 {
			t.Errorf("expected 2 files, got %d", len(infos))
		}

		// Check file names
		names := make(map[string]bool)
		for _, info := range infos {
			names[info.Path] = true
		}

		if !names["file1.txt"] || !names["file2.txt"] {
			t.Errorf("expected file1.txt and file2.txt, got %v", names)
		}
	})

	t.Run("read with offset and limit", func(t *testing.T) {
		// Create a multi-line file
		content := "line1\nline2\nline3\nline4\nline5"
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "multiline.txt",
			Content:  content,
		})
		if err != nil {
			t.Fatalf("write multiline file: %v", err)
		}

		// Read with offset
		result, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "multiline.txt",
			Offset:   3,
			Limit:    2,
		})
		if err != nil {
			t.Fatalf("read with offset/limit: %v", err)
		}

		expected := "line3\nline4"
		if result.Content != expected {
			t.Errorf("expected %q, got %q", expected, result.Content)
		}
	})

	t.Run("edit file", func(t *testing.T) {
		// Create a file
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "edit.txt",
			Content:  "Hello World",
		})
		if err != nil {
			t.Fatalf("write file for edit: %v", err)
		}

		// Edit it
		err = jail.Edit(ctx, &einofs.EditRequest{
			FilePath:   "edit.txt",
			OldString:  "World",
			NewString:  "Go",
			ReplaceAll: false,
		})
		if err != nil {
			t.Fatalf("edit file: %v", err)
		}

		// Read it back
		content, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "edit.txt",
		})
		if err != nil {
			t.Fatalf("read edited file: %v", err)
		}

		if content.Content != "Hello Go" {
			t.Errorf("expected 'Hello Go', got %q", content.Content)
		}
	})

	t.Run("grep search", func(t *testing.T) {
		// Create files with content
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "grep/file1.txt",
			Content:  "line one\nline two\nline three",
		})
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "grep/file2.txt",
			Content:  "match one\nmatch two\nno match",
		})

		// Search for "line"
		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{
			Pattern: "line",
			Path:    "grep",
		})
		if err != nil {
			t.Fatalf("grep search: %v", err)
		}

		if len(matches) != 3 {
			t.Errorf("expected 3 matches, got %d", len(matches))
		}

		// Check that all matches are within the jail
		for _, match := range matches {
			if !strings.HasPrefix(match.Path, "grep/") {
				t.Errorf("match path %q not within jail", match.Path)
			}
		}
	})

	t.Run("glob pattern", func(t *testing.T) {
		// Create files
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "glob/file1.txt",
			Content:  "content1",
		})
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "glob/file2.txt",
			Content:  "content2",
		})
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "glob/other.md",
			Content:  "content3",
		})

		// Glob for txt files
		infos, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{
			Pattern: "*.txt",
			Path:    "glob",
		})
		if err != nil {
			t.Fatalf("glob pattern: %v", err)
		}

		if len(infos) != 2 {
			t.Errorf("expected 2 txt files, got %d", len(infos))
		}

		// Check that all results are within the jail
		for _, info := range infos {
			if !strings.HasPrefix(info.Path, "glob/") {
				t.Errorf("glob result %q not within jail", info.Path)
			}
		}
	})
}

func TestFilesystemJail_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("empty root path", func(t *testing.T) {
		_, err := NewFilesystemJail("")
		if err == nil {
			t.Error("expected error for empty root, got nil")
		}
	})

	t.Run("non-existent root", func(t *testing.T) {
		_, err := NewFilesystemJail("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Error("expected error for non-existent root, got nil")
		}
	})

	t.Run("file instead of directory as root", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file.txt")
		_ = os.WriteFile(file, []byte("test"), 0644)

		_, err := NewFilesystemJail(file)
		if err == nil {
			t.Error("expected error for file as root, got nil")
		}
	})

	t.Run("edit with non-existent old string", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Create a file
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "edit.txt",
			Content:  "Hello World",
		})

		// Try to edit with non-existent old string
		err = jail.Edit(ctx, &einofs.EditRequest{
			FilePath:   "edit.txt",
			OldString:  "NonExistent",
			NewString:  "Replacement",
			ReplaceAll: false,
		})
		if err == nil {
			t.Error("expected error for non-existent old string, got nil")
		}
	})

	t.Run("edit with multiple occurrences and ReplaceAll=false", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Create a file with multiple occurrences
		_ = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "edit.txt",
			Content:  "cat cat cat",
		})

		// Try to edit with ReplaceAll=false when there are multiple occurrences
		err = jail.Edit(ctx, &einofs.EditRequest{
			FilePath:   "edit.txt",
			OldString:  "cat",
			NewString:  "dog",
			ReplaceAll: false,
		})
		if err == nil {
			t.Error("expected error for multiple occurrences with ReplaceAll=false, got nil")
		}
	})

	t.Run("empty path in request", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		// Try to read with empty path
		_, err = jail.Read(ctx, &einofs.ReadRequest{
			FilePath: "",
		})
		if err == nil {
			t.Error("expected error for empty path, got nil")
		}
	})
}

func TestFilesystemJail_EditReplaceAll(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	jail, err := NewFilesystemJail(root)
	if err != nil {
		t.Fatalf("create jail: %v", err)
	}

	// Create a file with multiple occurrences
	_ = jail.Write(ctx, &einofs.WriteRequest{
		FilePath: "replace.txt",
		Content:  "cat cat cat",
	})

	// Edit with ReplaceAll=true
	err = jail.Edit(ctx, &einofs.EditRequest{
		FilePath:   "replace.txt",
		OldString:  "cat",
		NewString:  "dog",
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("edit with ReplaceAll: %v", err)
	}

	// Read it back
	content, err := jail.Read(ctx, &einofs.ReadRequest{
		FilePath: "replace.txt",
	})
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}

	if content.Content != "dog dog dog" {
		t.Errorf("expected 'dog dog dog', got %q", content.Content)
	}
}

func TestFilesystemJail_ReadOnlyExtraRoots(t *testing.T) {
	ctx := context.Background()

	newJailWithSkills := func(t *testing.T) (einofs.Backend, string, string) {
		t.Helper()
		primary := t.TempDir()
		skillsRoot := t.TempDir()
		jail, err := NewFilesystemJailedWithRoots(primary, skillsRoot)
		if err != nil {
			t.Fatalf("create jail with roots: %v", err)
		}
		return jail, primary, skillsRoot
	}

	t.Run("absolute path under skills root reads OK", func(t *testing.T) {
		jail, primary, skillsRoot := newJailWithSkills(t)
		skillFile := filepath.Join(skillsRoot, "pdf-toolkit", "references", "usage.md")
		if err := os.MkdirAll(filepath.Dir(skillFile), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(skillFile, []byte("bundled content"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: skillFile})
		if err != nil {
			t.Fatalf("read by absolute path under extra root: %v", err)
		}
		if content.Content != "bundled content" {
			t.Errorf("content = %q, want %q", content.Content, "bundled content")
		}
		_ = primary
	})

	t.Run("write into skills root rejected", func(t *testing.T) {
		jail, _, skillsRoot := newJailWithSkills(t)

		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: filepath.Join(skillsRoot, "planted-skill", "SKILL.md"),
			Content:  "malicious",
		})
		if err == nil {
			t.Fatal("expected write into extra root to be rejected, got nil")
		}
		if _, statErr := os.Stat(filepath.Join(skillsRoot, "planted-skill")); statErr == nil {
			t.Error("write must not create files under the extra root")
		}
	})

	t.Run("edit in skills root rejected", func(t *testing.T) {
		jail, _, skillsRoot := newJailWithSkills(t)
		target := filepath.Join(skillsRoot, "existing.txt")
		if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		err := jail.Edit(ctx, &einofs.EditRequest{
			FilePath:  target,
			OldString: "original",
			NewString: "tampered",
		})
		if err == nil {
			t.Fatal("expected edit under extra root to be rejected, got nil")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "original" {
			t.Errorf("file must be unmodified, got %q", data)
		}
	})

	t.Run("symlink escape via extra root rejected", func(t *testing.T) {
		jail, _, skillsRoot := newJailWithSkills(t)

		outsideFile := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		escapeLink := filepath.Join(skillsRoot, "escape_link")
		if err := os.Symlink(outsideFile, escapeLink); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: escapeLink})
		if err == nil {
			t.Fatal("expected symlink escaping both roots to be rejected")
		}
		if !strings.Contains(err.Error(), "outside jail roots") && !strings.Contains(err.Error(), "escapes") {
			t.Errorf("expected escape error, got: %v", err)
		}
	})

	t.Run("absolute path outside all roots rejected", func(t *testing.T) {
		jail, _, _ := newJailWithSkills(t)
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: "/etc/passwd"})
		if err == nil {
			t.Fatal("expected absolute path outside roots to be rejected")
		}
		if !strings.Contains(err.Error(), "absolute") {
			t.Errorf("expected error mentioning 'absolute', got: %v", err)
		}
	})

	t.Run("missing extra root is skipped", func(t *testing.T) {
		primary := t.TempDir()
		missing := filepath.Join(t.TempDir(), "does", "not", "exist")
		jail, err := NewFilesystemJailedWithRoots(primary, missing)
		if err != nil {
			t.Fatalf("missing extra root must be tolerated: %v", err)
		}
		if err := jail.Write(ctx, &einofs.WriteRequest{FilePath: "ok.txt", Content: "x"}); err != nil {
			t.Errorf("primary root must remain writable: %v", err)
		}
	})

	t.Run("grep under skills root by absolute path", func(t *testing.T) {
		jail, _, skillsRoot := newJailWithSkills(t)
		if err := os.MkdirAll(filepath.Join(skillsRoot, "s"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(skillsRoot, "s", "a.md"), []byte("needle here"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{Pattern: "needle", Path: skillsRoot})
		if err != nil {
			t.Fatalf("grep under extra root: %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})
}

func TestFilesystemJail_MountPoint(t *testing.T) {
	ctx := context.Background()

	t.Run("write via mount lands under the primary root", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		err = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: DefaultMountPoint + "/lilianweng_latest_posts.md",
			Content:  "# hello",
		})
		if err != nil {
			t.Fatalf("write via mount: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, "lilianweng_latest_posts.md"))
		if err != nil {
			t.Fatalf("expected file under root: %v", err)
		}
		if string(data) != "# hello" {
			t.Errorf("unexpected content: %q", data)
		}
	})

	t.Run("write via mount creates nested directories", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		err = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: DefaultMountPoint + "/posts/2026/notes.md",
			Content:  "x",
		})
		if err != nil {
			t.Fatalf("write nested via mount: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "posts", "2026", "notes.md")); err != nil {
			t.Fatalf("expected nested file: %v", err)
		}
	})

	t.Run("read via mount", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("body"), 0644); err != nil {
			t.Fatalf("seed file: %v", err)
		}

		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: DefaultMountPoint + "/a.txt"})
		if err != nil {
			t.Fatalf("read via mount: %v", err)
		}
		if content.Content != "body" {
			t.Errorf("unexpected content: %q", content.Content)
		}
	})

	t.Run("mount .. escape still rejected", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		_, err = jail.Read(ctx, &einofs.ReadRequest{FilePath: DefaultMountPoint + "/../secrets.txt"})
		if err == nil || !strings.Contains(err.Error(), "..") {
			t.Errorf("expected .. rejection, got: %v", err)
		}
	})

	t.Run("raw absolute write still rejected", func(t *testing.T) {
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}

		err = jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "/tmp/onclaw-should-not-exist.md",
			Content:  "x",
		})
		if err == nil || !strings.Contains(err.Error(), "absolute paths are not allowed") {
			t.Errorf("expected absolute write rejection, got: %v", err)
		}
	})

	t.Run("write under the real root path is not reachable via mount confusion", func(t *testing.T) {
		// A path outside the mount and outside the root must not write.
		root := t.TempDir()
		jail, err := NewFilesystemJail(root)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}
		err = jail.Write(ctx, &einofs.WriteRequest{FilePath: "/workspacefoo/x.md", Content: "x"})
		if err == nil || !strings.Contains(err.Error(), "absolute paths are not allowed") {
			t.Errorf("expected prefix-confusion path rejection, got: %v", err)
		}
	})
}
