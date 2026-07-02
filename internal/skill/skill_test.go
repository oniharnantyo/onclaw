package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetDir(t *testing.T) {
	home := "/home/test"

	tgtGlobal := TargetDir(home, "global")
	if tgtGlobal != filepath.Join(home, "skills") {
		t.Errorf("expected target for global scope: %s, got: %s", filepath.Join(home, "skills"), tgtGlobal)
	}

	tgtAgent := TargetDir(home, "myagent")
	if tgtAgent != filepath.Join(home, "workspace", "myagent", ".agents", "skills") {
		t.Errorf("expected target for agent scope: %s, got: %s", filepath.Join(home, "workspace", "myagent", ".agents", "skills"), tgtAgent)
	}
}

func TestParseAndNormalizeManifest(t *testing.T) {
	// 1. Valid frontmatter
	content := `---
name: my-skill
description: Useful description
context: inline
custom_key: val
---
Body content here
`
	norm, meta, err := ParseAndNormalizeManifest(content, "fallback")
	if err != nil {
		t.Fatalf("unexpected error parsing valid frontmatter: %v", err)
	}
	if meta["name"] != "my-skill" || meta["description"] != "Useful description" || meta["custom_key"] != "val" {
		t.Errorf("metadata mismatch: %+v", meta)
	}
	if meta["context"] != "inline" {
		t.Errorf("expected context to be inline: %+v", meta)
	}

	// 2. Fork context (should be stripped)
	contentFork := `---
name: fork-skill
description: test
context: fork_with_context
---
Body
`
	norm, meta, err = ParseAndNormalizeManifest(contentFork, "fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := meta["context"]; exists {
		t.Error("expected context key starting with fork to be stripped from meta")
	}
	if strings.Contains(norm, "context: fork") {
		t.Error("expected context fork to be stripped from normalized text")
	}

	// 3. Missing name & description
	contentMissing := `
# A cool header
Some line explaining what this does.
`
	_, meta, err = ParseAndNormalizeManifest(contentMissing, "fallback-dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta["name"] != "fallback-dir" {
		t.Errorf("expected name to fallback to fallback-dir, got %v", meta["name"])
	}
	if meta["description"] != "A cool header" {
		t.Errorf("expected description to be synthesized, got %v", meta["description"])
	}
}

func TestDiscover(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-skills-discover")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested skills
	skill1Dir := filepath.Join(tmpDir, "group1", "skillA")
	if err := os.MkdirAll(skill1Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	skill1Content := `---
name: skillA
description: desc A
---
Body A
`
	if err := os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte(skill1Content), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// Nested skill 2
	skill2Dir := filepath.Join(tmpDir, "group2", "nested", "skillB")
	if err := os.MkdirAll(skill2Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	skill2Content := `---
name: skillB
description: desc B
---
Body B
`
	if err := os.WriteFile(filepath.Join(skill2Dir, "skill.md"), []byte(skill2Content), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// Discover all
	candidates, err := Discover(tmpDir, "")
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Verify names
	names := make(map[string]bool)
	for _, c := range candidates {
		names[c.Name] = true
	}
	if !names["skillA"] || !names["skillB"] {
		t.Errorf("expected skillA and skillB, got %+v", names)
	}

	// Discover with restrict
	candidatesRestricted, err := Discover(tmpDir, "group1")
	if err != nil {
		t.Fatalf("discover restricted failed: %v", err)
	}
	if len(candidatesRestricted) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidatesRestricted))
	}
	if candidatesRestricted[0].Name != "skillA" {
		t.Errorf("expected skillA, got %s", candidatesRestricted[0].Name)
	}
}
