package documents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSeedDocumentsSkill_CreatesWhenMissing covers the startup seed (design
// D10): the skill is written into the global skills tier with its
// frontmatter and playbook body.
func TestSeedDocumentsSkill_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedDocumentsSkill(dir))

	path := filepath.Join(dir, SkillName, "SKILL.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the seeded skill file must exist")
	content := string(raw)
	require.Contains(t, content, "name: documents")
	require.Contains(t, content, "read, revise, fill, or produce office documents",
		"the description states the activation trigger, including PDFs")
	require.Contains(t, content, "manipulate PDFs",
		"the description covers PDF manipulation")
	require.Contains(t, content, "read_file", "the playbook views a document before editing it")
	require.Contains(t, content, "/workspace/uploads", "the playbook points chat attachments at the mirrored uploads directory")
	require.NotContains(t, content, "save_document", "the playbook sheds the save-into-knowledge degradation framing (design D5)")
	require.Contains(t, content, "create_document", "the playbook names the generation and PDF-regeneration path")
}

// TestSeedDocumentsSkill_DoesNotOverwriteExisting pins repair-if-deleted
// semantics: an existing file — possibly user- or agent-edited — is never
// overwritten.
func TestSeedDocumentsSkill_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SkillName, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: documents\ndescription: my tailored playbook\n---\n"), 0o600))

	require.NoError(t, SeedDocumentsSkill(dir))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "my tailored playbook", "the existing file must stand")
}

// TestSeedDocumentsSkill_Idempotent covers the startup re-run: seeding twice
// leaves the same content.
func TestSeedDocumentsSkill_Idempotent(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SeedDocumentsSkill(dir))
	first, err := os.ReadFile(filepath.Join(dir, SkillName, "SKILL.md"))
	require.NoError(t, err)

	require.NoError(t, SeedDocumentsSkill(dir))
	second, err := os.ReadFile(filepath.Join(dir, SkillName, "SKILL.md"))
	require.NoError(t, err)

	require.Equal(t, string(first), string(second))
	require.True(t, strings.HasPrefix(string(second), "---\n"), "the skill keeps its frontmatter")
}
