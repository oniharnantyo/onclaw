package workspaces

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidSlug pins the DNS-label shape (design D3): lowercase
// alphanumerics, single interior dashes, 1..63 characters.
func TestValidSlug(t *testing.T) {
	valid := []string{
		"a",
		"0",
		"scout",
		"my-agent",
		"agent-2",
		"a-b-c",
		strings.Repeat("a", 63),
	}
	for _, slug := range valid {
		require.True(t, ValidSlug(slug), "slug %q must be valid", slug)
	}

	invalid := []string{
		"",                      // empty
		"Scout",                 // uppercase
		"-scout",                // leading dash
		"scout-",                // trailing dash
		"scout--two",            // doubled dash
		"sc out",                // space
		"scout_x",               // underscore
		"scoutñ",                // non-ASCII
		"a/b",                   // path separator
		strings.Repeat("a", 64), // one over the limit
	}
	for _, slug := range invalid {
		require.False(t, ValidSlug(slug), "slug %q must be invalid", slug)
	}
}

// TestSlugify covers the folding rules: lowercase, separator runs collapse
// to one dash, edge dashes trim, and over-long names cut at a dash boundary.
func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Scout", "scout"},
		{"My Agent", "my-agent"},
		{"  Trimmed  Name  ", "trimmed-name"},
		{"Weird___punctuation!!!here", "weird-punctuation-here"},
		{"Café Bot", "caf-bot"},        // non-ASCII folds to a separator
		{"123 numbers", "123-numbers"}, // digits are kept
		{"---", ""},                    // no alphanumerics: empty
		{"", ""},                       // empty input
		{"MiXeD CaSe", "mixed-case"},   // case folded
	}
	for _, tc := range cases {
		got := Slugify(tc.name)
		require.Equal(t, tc.want, got, "Slugify(%q)", tc.name)
		require.True(t, ValidSlug(got) || got == "", "Slugify(%q) = %q must be a valid slug or empty", tc.name, got)
	}
}

func TestSlugify_TruncatesLongNames(t *testing.T) {
	// No dash in range: hard cut at the limit.
	long := strings.Repeat("a", 100)
	require.Equal(t, strings.Repeat("a", 63), Slugify(long))

	// Dash boundary within range: cut there instead of mid-word.
	words := strings.Repeat("word-", 20) // 100 chars, dashes every 5
	got := Slugify(words)
	require.LessOrEqual(t, len(got), 63)
	require.True(t, ValidSlug(got))
	require.Equal(t, strings.Repeat("word-", 12)[:59], got, "must cut at the last dash boundary within 63")
}

// TestManager_DirAndLifecycle covers path resolution and the directory
// lifecycle: create makes the directory, remove deletes it, removing an
// absent workspace is a nil no-op, and invalid slugs are rejected before
// any filesystem access.
func TestManager_DirAndLifecycle(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)

	dir, err := m.Dir("scout")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "scout"), dir)

	_, err = m.Dir("../escape")
	require.Error(t, err, "invalid slugs must not resolve to a path")
	_, err = m.Dir("")
	require.Error(t, err)

	created, err := m.Create("scout")
	require.NoError(t, err)
	require.Equal(t, dir, created)
	info, err := os.Stat(created)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "workspaces stay private to the user")

	// Nested creation: the root itself may not exist yet either.
	nested, err := m.Create("fresh-agent")
	require.NoError(t, err)
	_, err = os.Stat(nested)
	require.NoError(t, err)

	require.NoError(t, m.Remove("scout"))
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err), "workspace must be gone after Remove")

	require.NoError(t, m.Remove("scout"), "removing an absent workspace is a no-op")

	require.Error(t, m.Remove("not valid!"), "invalid slugs must not touch the filesystem")
}

// TestManager_ReadDocument covers the absent-document convention and the
// read path.
func TestManager_ReadDocument(t *testing.T) {
	m := NewManager(t.TempDir())
	dir, err := m.Create("scout")
	require.NoError(t, err)

	content, exists, err := m.ReadDocument(dir, DocSoul)
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, content)

	require.NoError(t, os.WriteFile(filepath.Join(dir, DocSoul), []byte("# soul"), 0600))
	content, exists, err = m.ReadDocument(dir, DocSoul)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "# soul", content)
}

// TestManager_WriteDocumentWithBackup covers the write path: a first write
// creates the document and no backup, an overwrite keeps the previous
// content as <name>.bak and replaces the document, and each overwrite
// refreshes the backup to the immediately-previous content.
func TestManager_WriteDocumentWithBackup(t *testing.T) {
	m := NewManager(t.TempDir())
	dir, err := m.Create("scout")
	require.NoError(t, err)
	target := filepath.Join(dir, DocIdentity)

	// First write: no prior document, so no backup must appear.
	require.NoError(t, m.WriteDocumentWithBackup(dir, DocIdentity, "v1"))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "v1", string(got))
	_, err = os.Stat(target + ".bak")
	require.True(t, os.IsNotExist(err), "a first write must not create a backup")

	// Overwrite: previous content lands in the backup.
	require.NoError(t, m.WriteDocumentWithBackup(dir, DocIdentity, "v2"))
	got, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "v2", string(got), "content must be replaced")
	got, err = os.ReadFile(target + ".bak")
	require.NoError(t, err)
	require.Equal(t, "v1", string(got), "backup must hold the previous content")

	// Second overwrite: the backup refreshes to the immediately-previous
	// content, not the oldest one.
	require.NoError(t, m.WriteDocumentWithBackup(dir, DocIdentity, "v3"))
	got, err = os.ReadFile(target + ".bak")
	require.NoError(t, err)
	require.Equal(t, "v2", string(got))
}

// TestManager_BackupFailureAborts pins design D6: when the backup cannot be
// written the whole call fails and the target document keeps its original
// content. The reliable POSIX simulation is a directory parked at the
// backup path — creating the backup then fails with EISDIR while nothing
// about the target write path is disturbed.
func TestManager_BackupFailureAborts(t *testing.T) {
	m := NewManager(t.TempDir())
	dir, err := m.Create("scout")
	require.NoError(t, err)
	target := filepath.Join(dir, DocIdentity)
	require.NoError(t, os.WriteFile(target, []byte("original"), 0600))

	// A directory where the backup file must go: backup creation fails.
	require.NoError(t, os.Mkdir(target+".bak", 0700))

	err = m.WriteDocumentWithBackup(dir, DocIdentity, "enhanced")
	require.Error(t, err, "a backup failure must abort the write")

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "original", string(got), "the target must keep its pre-call content")

	// No temp file residue from the aborted attempt.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "only the document and the backup directory may remain")
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "."), "no temp files may be left behind")
	}
}

// TestManager_SkillsDirAndTasksDir pins the on-demand subdirectory layout
// (design D6): SkillsDir and TasksDir mkdir -p their directory inside the
// workspace and are idempotent.
func TestManager_SkillsDirAndTasksDir(t *testing.T) {
	m := NewManager(t.TempDir())

	skills, err := m.SkillsDir("scout")
	require.NoError(t, err)
	require.DirExists(t, skills, "SkillsDir must create the directory on demand")
	require.Equal(t, filepath.Join(m.Root(), "scout", DirSkills), skills)

	tasks, err := m.TasksDir("scout")
	require.NoError(t, err)
	require.DirExists(t, tasks, "TasksDir must create the directory on demand")
	require.Equal(t, filepath.Join(m.Root(), "scout", DirTasks), tasks)

	// Idempotent: a second call succeeds and returns the same path.
	again, err := m.SkillsDir("scout")
	require.NoError(t, err)
	require.Equal(t, skills, again)

	// The workspace directory itself came into being alongside them.
	require.DirExists(t, filepath.Join(m.Root(), "scout"))

	// An invalid slug fails exactly like Dir/Create do.
	_, err = m.SkillsDir("../escape")
	require.Error(t, err)
	_, err = m.TasksDir("")
	require.Error(t, err)
}

// TestManager_ArtifactsDir pins the deliverables home (add-turn-artifacts
// design D1): ArtifactsDir mkdir -p's artifacts/ inside the workspace, is
// idempotent, and refuses an invalid slug like the other subdirectories.
func TestManager_ArtifactsDir(t *testing.T) {
	m := NewManager(t.TempDir())

	artifacts, err := m.ArtifactsDir("scout")
	require.NoError(t, err)
	require.DirExists(t, artifacts, "ArtifactsDir must create the directory on demand")
	require.Equal(t, filepath.Join(m.Root(), "scout", DirArtifacts), artifacts)

	again, err := m.ArtifactsDir("scout")
	require.NoError(t, err)
	require.Equal(t, artifacts, again)

	_, err = m.ArtifactsDir("../escape")
	require.Error(t, err)
	_, err = m.ArtifactsDir("")
	require.Error(t, err)
}

// TestManager_UploadsDir pins the uploads mirror home
// (fix-pdf-attachment-carriage design D2): UploadsDir mkdir -p's uploads/
// inside the workspace, is idempotent, and refuses an invalid slug like the
// other subdirectories.
func TestManager_UploadsDir(t *testing.T) {
	m := NewManager(t.TempDir())

	uploads, err := m.UploadsDir("scout")
	require.NoError(t, err)
	require.DirExists(t, uploads, "UploadsDir must create the directory on demand")
	require.Equal(t, filepath.Join(m.Root(), "scout", DirUploads), uploads)

	again, err := m.UploadsDir("scout")
	require.NoError(t, err)
	require.Equal(t, uploads, again)

	_, err = m.UploadsDir("../escape")
	require.Error(t, err)
	_, err = m.UploadsDir("")
	require.Error(t, err)
}
