package middlewares

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

func TestResolveDirs(t *testing.T) {
	home := "/home/test"
	agent := "myagent"

	dirs := ResolveDirs(home, agent)
	expectedDirs := []string{
		filepath.Join(home, "workspace", agent, "skills"),
		filepath.Join(home, "workspace", agent, ".agents", "skills"),
		filepath.Join(home, "skills"),
	}

	if len(dirs) != len(expectedDirs) {
		t.Fatalf("expected %d directories, got %d", len(expectedDirs), len(dirs))
	}
	for i, d := range dirs {
		if d != expectedDirs[i] {
			t.Errorf("dir at %d mismatch. got: %s, want: %s", i, d, expectedDirs[i])
		}
	}
}

func TestBackendPrecedence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-skills-backend")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Precedence order: dir1, then dir2
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")

	if err := os.MkdirAll(filepath.Join(dir1, "skillX"), 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir2, "skillX"), 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir2, "skillY"), 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}

	// Write skillX in dir1 (higher priority)
	contentX1 := `---
name: skillX
description: Description X from dir1
---
Body X1
`
	if err := os.WriteFile(filepath.Join(dir1, "skillX", "SKILL.md"), []byte(contentX1), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Write skillX in dir2 (lower priority)
	contentX2 := `---
name: skillX
description: Description X from dir2
---
Body X2
`
	if err := os.WriteFile(filepath.Join(dir2, "skillX", "SKILL.md"), []byte(contentX2), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Write skillY in dir2
	contentY := `---
name: skillY
description: Description Y from dir2
---
Body Y
`
	if err := os.WriteFile(filepath.Join(dir2, "skillY", "SKILL.md"), []byte(contentY), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Setup backend
	backend := NewMultiDirBackend([]string{dir1, dir2})
	ctx := context.Background()

	// List
	list, err := backend.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(list))
	}

	var fmX, fmY *einoskill.FrontMatter
	for i := range list {
		if list[i].Name == "skillX" {
			fmX = &list[i]
		} else if list[i].Name == "skillY" {
			fmY = &list[i]
		}
	}

	if fmX == nil || fmY == nil {
		t.Fatal("could not find both skillX and skillY in list")
	}

	// Verify precedence: skillX description must come from dir1
	if fmX.Description != "Description X from dir1" {
		t.Errorf("precedence check failed, expected description from dir1, got: %s", fmX.Description)
	}

	// Get skillX
	skX, err := backend.Get(ctx, "skillX")
	if err != nil {
		t.Fatalf("get skillX failed: %v", err)
	}
	if strings.TrimSpace(skX.Content) != "Body X1" {
		t.Errorf("expected body X1, got: %q", skX.Content)
	}

	// Get skillY
	skY, err := backend.Get(ctx, "skillY")
	if err != nil {
		t.Fatalf("get skillY failed: %v", err)
	}
	if strings.TrimSpace(skY.Content) != "Body Y" {
		t.Errorf("expected body Y, got: %q", skY.Content)
	}
}