package systemskills

import (
	"strings"
	"testing"
)

// TestDocumentReadSkillContent is the content contract for the embedded
// document-read skill (add-document-read-skill task 2.2): the tool names,
// the mount path, and the never-via-shell rule are the stable family
// contract, and the header must carry a valid name/description pair so the
// skills middleware can advertise it.
func TestDocumentReadSkillContent(t *testing.T) {
	content, err := GetEmbeddedSkill("document-read")
	if err != nil {
		t.Fatalf("GetEmbeddedSkill(document-read) failed: %v", err)
	}

	t.Run("valid name/description header", func(t *testing.T) {
		name, description := skillHeader(content)

		if name != "document-read" {
			t.Errorf("expected name header to be document-read, got %q", name)
		}
		if description == "" {
			t.Error("expected a non-empty description header line")
		}
	})

	t.Run("mentions the document tool family and mount path", func(t *testing.T) {
		for _, want := range []string{"document.search", "document.read", "references/"} {
			if !strings.Contains(content, want) {
				t.Errorf("expected skill content to mention %q", want)
			}
		}
	})

	t.Run("carries the never-shell rule", func(t *testing.T) {
		for _, want := range []string{"## Never via shell", "shell", "Glob", "unzip"} {
			if !strings.Contains(content, want) {
				t.Errorf("expected never-via-shell section to mention %q", want)
			}
		}
	})

	t.Run("teaches multi-document delegation", func(t *testing.T) {
		if !strings.Contains(content, "`agent` tool") {
			t.Error("expected skill content to teach delegation via the `agent` tool")
		}
	})
}

// skillHeader extracts the name: and description: values from a SKILL.md's
// header lines (the web-research template format: # Title, then the two
// key: value lines).
func skillHeader(content string) (name, description string) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case name == "" && strings.HasPrefix(trimmed, "name:"):
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		case description == "" && strings.HasPrefix(trimmed, "description:"):
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
		if name != "" && description != "" {
			return name, description
		}
	}
	return name, description
}
