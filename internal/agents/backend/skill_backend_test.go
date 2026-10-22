package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillBackend_Discovery(t *testing.T) {
	ctx := context.Background()

	t.Run("frontmatter description and absent tiers", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		agentDir := filepath.Join(tmpDir, "agent", "skills")
		// Written verbatim to pin the spec's SKILL.md format.
		skillDir := filepath.Join(globalDir, "trip-planner")
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "---\nname: trip-planner\ndescription: Plan trips end to end.\n---\n# Trip planner\nBody.\n"
		if err := os.WriteFile(filepath.Join(skillDir, SkillMDFileName), []byte(body), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		resolver := NewSkillBackend(globalDir, agentDir, nil, nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(metadata))
		}
		if metadata[0].Name != "trip-planner" {
			t.Errorf("expected name 'trip-planner', got %s", metadata[0].Name)
		}
		if metadata[0].Description != "Plan trips end to end." {
			t.Errorf("expected frontmatter description, got %q", metadata[0].Description)
		}

		// The agent tier directory does not exist yet: the absent tier is the
		// normal empty state, not an error.
		names, err := resolver.AvailableSkillNames(ctx)
		if err != nil {
			t.Fatalf("AvailableSkillNames failed: %v", err)
		}
		if len(names) != 1 || names[0] != "trip-planner" {
			t.Errorf("expected [trip-planner], got %v", names)
		}
	})

	t.Run("directories without SKILL.md are skipped", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		writeSkill(t, globalDir, "real-skill", "# real\nA real skill body.\n")
		if err := os.MkdirAll(filepath.Join(globalDir, "not-a-skill"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		resolver := NewSkillBackend(globalDir, filepath.Join(tmpDir, "agent", "skills"), nil, nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 1 || metadata[0].Name != "real-skill" {
			t.Fatalf("expected only real-skill, got %v", metadata)
		}
	})

	t.Run("missing global directory is the empty state", func(t *testing.T) {
		resolver := NewSkillBackend(filepath.Join(t.TempDir(), "absent"), filepath.Join(t.TempDir(), "absent"), nil, nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 0 {
			t.Fatalf("expected no skills, got %v", metadata)
		}
	})
}

func TestSkillBackend_Precedence(t *testing.T) {
	ctx := context.Background()

	t.Run("agent tier overrides global on name conflict", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		agentDir := filepath.Join(tmpDir, "agent", "skills")
		writeSkill(t, globalDir, "triage", "Global version")
		writeSkill(t, agentDir, "triage", "Agent version")
		writeSkill(t, globalDir, "global-only", "Global only")

		resolver := NewSkillBackend(globalDir, agentDir, nil, nil)

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 2 {
			t.Fatalf("expected 2 skills, got %d", len(metadata))
		}
		// Sorted by name, the agent tier's description wins the conflict.
		if metadata[0].Name != "global-only" || metadata[0].Description != "Global only" {
			t.Errorf("expected global-only untouched, got %+v", metadata[0])
		}
		if metadata[1].Name != "triage" || metadata[1].Description != "Agent version" {
			t.Errorf("expected agent tier to win, got %+v", metadata[1])
		}

		body, err := resolver.Get(ctx, "triage")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !strings.Contains(body.Content, "Agent version") {
			t.Errorf("expected agent tier body, got %s", body.Content)
		}
		if body.BaseDirectory != filepath.Join(agentDir, "triage") {
			t.Errorf("expected agent tier base directory, got %s", body.BaseDirectory)
		}
	})

	t.Run("agent skill shadows disabled global and is itself gated", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		agentDir := filepath.Join(tmpDir, "agent", "skills")
		writeSkill(t, globalDir, "shared", "Global version")
		writeSkill(t, agentDir, "shared", "Agent version")

		// Name-level disable: a disabled name is out across both tiers, so
		// the agent tier cannot resurrect it.
		resolver := NewSkillBackend(globalDir, agentDir, staticEligibility{"shared": {}}, nil)
		if metadata, err := resolver.List(ctx); err != nil || len(metadata) != 0 {
			t.Fatalf("expected no skills for a disabled name, got %v (err %v)", metadata, err)
		}
		if _, err := resolver.Get(ctx, "shared"); err == nil {
			t.Error("Get must reject a disabled name across tiers")
		}
	})
}

func TestSkillBackend_LivePickup(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	agentDir := filepath.Join(tmpDir, "agent", "skills")
	resolver := NewSkillBackend(globalDir, agentDir, nil, nil)

	before, err := resolver.List(ctx)
	if err != nil {
		t.Fatalf("initial List failed: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected an empty start, got %v", before)
	}

	// Drop a new skill into the workspace skills directory: the next resolve
	// sees it without a restart (fresh read per turn, design D6).
	writeSkill(t, agentDir, "hot-skill", "# hot\nFreshly dropped.\n")
	after, err := resolver.List(ctx)
	if err != nil {
		t.Fatalf("post-write List failed: %v", err)
	}
	if len(after) != 1 || after[0].Name != "hot-skill" {
		t.Fatalf("expected the new skill live, got %v", after)
	}

	// An edit on disk applies to the next read for free.
	writeSkill(t, agentDir, "hot-skill", "# hot\nEdited description line.\n")
	body, err := resolver.Get(ctx, "hot-skill")
	if err != nil {
		t.Fatalf("Get after edit failed: %v", err)
	}
	if !strings.Contains(body.Content, "Edited description line.") {
		t.Errorf("expected the edited body, got %s", body.Content)
	}
}

func TestSkillBackend_Eligibility(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled skill never offered and never loads", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		agentDir := filepath.Join(tmpDir, "agent", "skills")
		writeSkill(t, globalDir, "enabled-skill", "Enabled skill")
		writeSkill(t, globalDir, "disabled-skill", "Disabled skill")
		writeSkill(t, agentDir, "agent-skill", "Agent skill")

		resolver := NewSkillBackend(globalDir, agentDir, staticEligibility{"disabled-skill": {}}, nil)

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		names := make(map[string]bool, len(metadata))
		for _, m := range metadata {
			names[m.Name] = true
		}
		if names["disabled-skill"] {
			t.Error("a globally disabled skill must not be offered")
		}
		if !names["enabled-skill"] || !names["agent-skill"] {
			t.Errorf("expected enabled-skill and agent-skill, got %v", names)
		}

		if _, err := resolver.Get(ctx, "disabled-skill"); err == nil {
			t.Error("$-invocation of a disabled skill must not load a body")
		}
		if names, err := resolver.AvailableSkillNames(ctx); err != nil {
			t.Fatalf("AvailableSkillNames failed: %v", err)
		} else {
			for _, n := range names {
				if n == "disabled-skill" {
					t.Errorf("disabled skill must not be available, got %v", names)
				}
			}
		}
	})

	t.Run("nil reader treats every skill as enabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		writeSkill(t, globalDir, "any-skill", "Any skill")

		resolver := NewSkillBackend(globalDir, filepath.Join(tmpDir, "agent", "skills"), nil, nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 1 || metadata[0].Name != "any-skill" {
			t.Fatalf("expected any-skill with nil reader, got %v", metadata)
		}
	})

	t.Run("reader error surfaces from List and Get", func(t *testing.T) {
		tmpDir := t.TempDir()
		globalDir := filepath.Join(tmpDir, "global")
		writeSkill(t, globalDir, "ws-skill", "Skill")

		resolver := NewSkillBackend(globalDir, filepath.Join(tmpDir, "agent", "skills"), errorEligibility{}, nil)
		if _, err := resolver.List(ctx); err == nil {
			t.Error("expected List to fail when the eligibility reader errors")
		}
		if _, err := resolver.Get(ctx, "ws-skill"); err == nil {
			t.Error("expected Get to fail when the eligibility reader errors")
		}
	})
}

func TestSkillBackend_Allowlist(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	agentDir := filepath.Join(tmpDir, "agent", "skills")
	writeSkill(t, globalDir, "alpha", "Alpha")
	writeSkill(t, globalDir, "beta", "Beta")
	writeSkill(t, agentDir, "gamma", "Gamma")

	t.Run("empty allowlist permits all", func(t *testing.T) {
		resolver := NewSkillBackend(globalDir, agentDir, nil, nil)
		names, err := resolver.AvailableSkillNames(ctx)
		if err != nil {
			t.Fatalf("AvailableSkillNames failed: %v", err)
		}
		if len(names) != 3 {
			t.Fatalf("expected all three skills, got %v", names)
		}
	})

	t.Run("allowlist narrows the offered set and body loads", func(t *testing.T) {
		resolver := NewSkillBackend(globalDir, agentDir, nil, []string{"alpha", "gamma"})

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 2 || metadata[0].Name != "alpha" || metadata[1].Name != "gamma" {
			t.Fatalf("expected [alpha gamma], got %v", metadata)
		}
		if _, err := resolver.Get(ctx, "beta"); err == nil {
			t.Error("a non-allowlisted skill must not load a body")
		}
		if _, err := resolver.Get(ctx, "alpha"); err != nil {
			t.Errorf("an allowlisted skill must load: %v", err)
		}
	})
}

func TestSkillBackend_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("get skill from agent tier", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentDir := filepath.Join(tmpDir, "agent", "skills")
		writeSkill(t, agentDir, "agent-skill", "Agent skill content")

		resolver := NewSkillBackend(filepath.Join(tmpDir, "global"), agentDir, nil, nil)
		body, err := resolver.Get(ctx, "agent-skill")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if body.Name != "agent-skill" {
			t.Errorf("expected name 'agent-skill', got %s", body.Name)
		}
		if !strings.Contains(body.Content, "Agent skill content") {
			t.Errorf("expected content to contain 'Agent skill content', got %s", body.Content)
		}
	})

	t.Run("get non-existent skill returns error", func(t *testing.T) {
		resolver := NewSkillBackend(filepath.Join(t.TempDir(), "g"), filepath.Join(t.TempDir(), "a"), nil, nil)
		if _, err := resolver.Get(ctx, "non-existent"); err == nil {
			t.Error("expected error for non-existent skill, got nil")
		}
	})
}

func TestParseSkillDescription(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "frontmatter description",
			content: "---\nname: pdf\ndescription: PDF tools.\n---\n# PDF\nBody.",
			want:    "PDF tools.",
		},
		{
			name:    "no frontmatter falls back to first body line",
			content: "# Heading\nFirst meaningful line.\n",
			want:    "First meaningful line.",
		},
		{
			name:    "frontmatter without description falls back",
			content: "---\nname: pdf\n---\n# PDF\nFallback line.\n",
			want:    "Fallback line.",
		},
		{
			name:    "unterminated frontmatter treated as body",
			content: "description: body fallback\nno closing marker",
			want:    "description: body fallback",
		},
		{
			name:    "crlf frontmatter marker",
			content: "---\r\ndescription: Windows line endings.\r\n---\r\nbody",
			want:    "Windows line endings.",
		},
		{
			name:    "nothing meaningful degrades to the default",
			content: "#\n\n---\n",
			want:    "A skill",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseSkillDescription(tc.content); got != tc.want {
				t.Errorf("ParseSkillDescription() = %q, want %q", got, tc.want)
			}
		})
	}
}

// staticEligibility is a fixed SkillEligibility fake: exactly these names
// are disabled.
type staticEligibility map[string]struct{}

func (s staticEligibility) DisabledSkillNames(ctx context.Context) ([]string, error) {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	return names, nil
}

// errorEligibility always fails, to exercise error propagation.
type errorEligibility struct{}

func (errorEligibility) DisabledSkillNames(ctx context.Context) ([]string, error) {
	return nil, errors.New("registry unavailable")
}

// writeSkill creates one skill directory with a spec-shaped SKILL.md: a
// leading frontmatter block carrying the name and description.
func writeSkill(t *testing.T, dir, name, description string) {
	t.Helper()

	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\n" + description + "\n\nThis is a test skill.\n"
	if err := os.WriteFile(filepath.Join(skillDir, SkillMDFileName), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}
}
