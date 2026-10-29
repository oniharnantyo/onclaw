package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/stretchr/testify/require"
)

// The memory skill seed tests (add-agent-memory tasks 3.1, spec agent-memory
// "Memory skill"): seeding when absent, no overwrite when present, and the
// agent tier shadowing the seeded global skill through the existing two-tier
// discovery.

// TestSeedSkill_CreatesWhenMissing covers the startup seed: the skill is
// written into the global skills tier with its frontmatter and playbook
// body — the read-first recall path, the write conventions, and the
// activation trigger naming preferences, past decisions, and earlier work.
func TestSeedSkill_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedSkill(dir))

	path := filepath.Join(dir, SkillName, "SKILL.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the seeded skill file must exist")
	content := string(raw)
	require.Contains(t, content, "name: memory")
	require.Contains(t, content, "preferences, past decisions, or earlier work",
		"the description states the activation trigger")
	require.Contains(t, content, "MEMORY.md", "the playbook reads the index first")
	require.Contains(t, content, "memory_search", "the playbook names the search tool")
	require.Contains(t, content, "supersedes", "the playbook names the supersede convention")
	require.Contains(t, content, "memory/YYYY-MM-DD.md", "the playbook names the daily working note")
}

// TestSeedSkill_DoesNotOverwriteExisting pins repair-if-deleted semantics: an
// existing file — possibly user- or agent-edited — is never overwritten.
func TestSeedSkill_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SkillName, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: memory\ndescription: my tailored playbook\n---\n"), 0600))

	require.NoError(t, SeedSkill(dir))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "my tailored playbook", "the existing file must stand")
}

// TestSeedSkill_AgentTierShadowsGlobal seeds the global tier, drops an
// agent-tier skill of the same name, and resolves both through the existing
// two-tier discovery (backend.NewSkillBackend): the workspace version wins
// on Get and is the only one advertised on List.
func TestSeedSkill_AgentTierShadowsGlobal(t *testing.T) {
	globalDir := t.TempDir()
	require.NoError(t, SeedSkill(globalDir))

	agentDir := t.TempDir()
	shadowed := "---\nname: memory\ndescription: the agent's own memory playbook\n---\n\nAgent-tier body.\n"
	require.NoError(t, os.MkdirAll(filepath.Join(agentDir, SkillName), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, SkillName, "SKILL.md"), []byte(shadowed), 0600))

	b := backend.NewSkillBackend(globalDir, agentDir, nil, nil)
	skill, err := b.Get(context.Background(), SkillName)
	require.NoError(t, err, "the shadowing skill must resolve")
	require.Equal(t, filepath.Join(agentDir, SkillName), skill.BaseDirectory,
		"the agent tier wins the name conflict")
	require.Contains(t, skill.Content, "Agent-tier body.", "the agent-tier body is served, not the seeded one")
	require.Contains(t, skill.Content, "the agent's own memory playbook")
	require.NotContains(t, skill.Content, "MEMORY.md", "the seeded global body must not ride along")

	metadata, err := b.List(context.Background())
	require.NoError(t, err)
	require.Len(t, metadata, 1, "the shadowed name advertises exactly once")
	require.Equal(t, SkillName, metadata[0].Name)
	require.Contains(t, metadata[0].Description, "the agent's own memory playbook")

	// Without the agent tier, the seeded global skill resolves instead.
	globalOnly := backend.NewSkillBackend(globalDir, t.TempDir(), nil, nil)
	skill, err = globalOnly.Get(context.Background(), SkillName)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(globalDir, SkillName), skill.BaseDirectory)
	require.True(t, strings.Contains(skill.Content, "MEMORY.md"), "the seeded playbook backs the global tier")
}
