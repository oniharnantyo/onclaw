package skill_test

import (
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

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
	norm, meta, err := skill.ParseAndNormalizeManifest(content, "fallback")
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
	norm, meta, err = skill.ParseAndNormalizeManifest(contentFork, "fallback")
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
	_, meta, err = skill.ParseAndNormalizeManifest(contentMissing, "fallback-dir")
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
