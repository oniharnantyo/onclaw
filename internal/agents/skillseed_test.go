package agents

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The create-skill seed tests (add-agent-created-skills tasks 1.3, spec
// agent-skills "Seed does not clobber"): seeding when absent, a no-op on an
// already-seeded layout, and an existing skill of the same name standing.

// TestSeedCreateSkill_CreatesWhenMissing covers the startup seed: the skill
// is written into the global skills tier with exactly the playbook content —
// the directory is created on demand and the file carries the seeded bytes.
func TestSeedCreateSkill_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedCreateSkill(dir))

	path := filepath.Join(dir, SkillName, "SKILL.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the seeded skill file must exist")
	require.Equal(t, skillContent, string(raw), "the seeded file carries the playbook exactly")
}

// TestSeedCreateSkill_Idempotent covers the no-op: calling the seed again on
// an already-seeded layout rewrites nothing — the file's modification time
// is pinned to the past before reseeding, so a rewrite would move it.
func TestSeedCreateSkill_Idempotent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SeedCreateSkill(dir))
	path := filepath.Join(dir, SkillName, "SKILL.md")
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, past, past))

	require.NoError(t, SeedCreateSkill(dir))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.ModTime().Equal(past), "reseeding a seeded layout must not rewrite the file")
}

// TestSeedCreateSkill_DoesNotOverwriteExisting pins repair-if-deleted
// semantics (spec agent-skills "Seed does not clobber"): an existing file —
// possibly user- or agent-tailored — is never overwritten.
func TestSeedCreateSkill_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SkillName, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	tailored := "---\nname: create-skill\ndescription: my tailored authoring playbook\n---\n\nHouse rules stand.\n"
	require.NoError(t, os.WriteFile(path, []byte(tailored), 0o600))

	require.NoError(t, SeedCreateSkill(dir))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, tailored, string(raw), "the existing skill must stand")
}
