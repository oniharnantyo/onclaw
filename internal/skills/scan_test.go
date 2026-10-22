package skills

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// writeSkillMD creates a skill directory with a SKILL.md frontmatter file.
func writeSkillMD(t *testing.T, dir, name, description string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o700))
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, SkillMD), []byte(content), 0o600))
}

// TestScan_UpsertsDiscoveredSkills covers the happy path: skill directories
// are discovered, parsed, and upserted as directory-sourced rows.
func TestScan_UpsertsDiscoveredSkills(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeSkillMD(t, dir, "summarize", "Summarize long texts.")
	writeSkillMD(t, dir, "translate", "Translate between languages.")
	// Non-directory entries and directories without SKILL.md are indexed
	// only when they are directories; a stray file is ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o700))

	reg := fake.New()
	require.NoError(t, Scan(ctx, reg.Skills(), dir, slog.Default()))

	skills, err := reg.Skills().List(ctx)
	require.NoError(t, err)
	require.Len(t, skills, 3) // summarize, translate, empty (bare directory)

	byName := make(map[string]store.Skill, len(skills))
	for _, skill := range skills {
		byName[skill.Name] = skill
	}
	require.Equal(t, "Summarize long texts.", byName["summarize"].Description)
	require.Equal(t, SourceDirectory, byName["summarize"].Source)
	require.True(t, byName["summarize"].Enabled)
	require.Equal(t, "Translate between languages.", byName["translate"].Description)
	require.Equal(t, "", byName["empty"].Description)
}

// TestScan_PreservesEnabledToggle proves a rescan never flips the user's
// enable state.
func TestScan_PreservesEnabledToggle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeSkillMD(t, dir, "summarize", "Summarize long texts.")

	reg := fake.New()
	require.NoError(t, Scan(ctx, reg.Skills(), dir, slog.Default()))
	require.NoError(t, reg.Skills().SetEnabled(ctx, "summarize", false))

	require.NoError(t, Scan(ctx, reg.Skills(), dir, slog.Default()))
	skill, err := reg.Skills().GetByName(ctx, "summarize")
	require.NoError(t, err)
	require.False(t, skill.Enabled)
}

// TestScan_MissingDirectoryIsNoOp keeps a fresh install booting cleanly.
func TestScan_MissingDirectoryIsNoOp(t *testing.T) {
	reg := fake.New()
	require.NoError(t, Scan(context.Background(), reg.Skills(),
		filepath.Join(t.TempDir(), "does-not-exist"), slog.Default()))
	skills, err := reg.Skills().List(context.Background())
	require.NoError(t, err)
	require.Empty(t, skills)
}

// TestScan_ToleratesBadDirectories proves one broken skill directory is
// skipped with a log while the healthy ones still index.
func TestScan_ToleratesBadDirectories(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeSkillMD(t, dir, "healthy", "A healthy skill.")

	// A skill directory whose SKILL.md is itself a directory: reading it
	// fails, which the scanner must skip.
	bad := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(filepath.Join(bad, SkillMD), 0o700))

	reg := fake.New()
	require.NoError(t, Scan(ctx, reg.Skills(), dir, slog.Default()))

	skills, err := reg.Skills().List(ctx)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "healthy", skills[0].Name)
}

// TestParseFrontmatter pins the SKILL.md frontmatter convention.
func TestParseFrontmatter(t *testing.T) {
	t.Run("standard block", func(t *testing.T) {
		name, description := parseFrontmatter("---\nname: my-skill\ndescription: Does things.\n---\n\nBody")
		require.Equal(t, "my-skill", name)
		require.Equal(t, "Does things.", description)
	})
	t.Run("no frontmatter", func(t *testing.T) {
		name, description := parseFrontmatter("# Just markdown")
		require.Empty(t, name)
		require.Empty(t, description)
	})
	t.Run("unclosed block yields keys before EOF", func(t *testing.T) {
		name, description := parseFrontmatter("---\nname: x\ndescription: y\n")
		require.Equal(t, "x", name)
		require.Equal(t, "y", description)
	})
}
