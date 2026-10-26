package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSeedSkill_CreatesWhenMissing covers the startup seed (design D10): the
// skill is written into the global skills tier with its frontmatter and
// playbook body.
func TestSeedSkill_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedSkill(dir))

	path := filepath.Join(dir, SkillName, "SKILL.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the seeded skill file must exist")
	content := string(raw)
	require.Contains(t, content, "name: knowledge-search")
	require.Contains(t, content, "Use whenever the user asks about, quotes, or references saved documents",
		"the description states the activation trigger")
	require.Contains(t, content, "/knowledge/global/INDEX.md", "the playbook reads the scope indexes first")
	require.Contains(t, content, "save_document", "the playbook names the persist path")
}

// TestSeedSkill_DoesNotOverwriteExisting pins repair-if-deleted semantics: an
// existing file — possibly user- or agent-edited — is never overwritten.
func TestSeedSkill_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SkillName, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: knowledge-search\ndescription: my tailored playbook\n---\n"), 0600))

	require.NoError(t, SeedSkill(dir))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "my tailored playbook", "the existing file must stand")
}

// TestSeedSkill_Idempotent covers the startup re-run: seeding twice leaves
// the same content.
func TestSeedSkill_Idempotent(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedSkill(dir))
	first, err := os.ReadFile(filepath.Join(dir, SkillName, "SKILL.md"))
	require.NoError(t, err)

	require.NoError(t, SeedSkill(dir))
	second, err := os.ReadFile(filepath.Join(dir, SkillName, "SKILL.md"))
	require.NoError(t, err)

	require.Equal(t, string(first), string(second))
	require.True(t, strings.HasPrefix(string(second), "---\n"), "the skill keeps its frontmatter")
}
