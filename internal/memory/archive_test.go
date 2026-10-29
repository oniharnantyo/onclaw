package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeNote writes one fact note and returns its path.
func writeNote(t *testing.T, path, raw string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(raw), 0600))
	return path
}

const archiveTestNote = `---
name: stale-fact
description: A fact whose time has passed
type: project
---

The staging database runs on port 5432.
`

// TestArchiveAndRestore_RoundTrip covers both directions of the archive
// contract: archiving moves the note into memory/archive/ with its content
// intact, and restoring moves it back.
func TestArchiveAndRestore_RoundTrip(t *testing.T) {
	layout := NewLayout(t.TempDir())
	require.NoError(t, EnsureLayout(layout))
	src := writeNote(t, layout.NotePath("stale-fact"), archiveTestNote)
	original, err := os.ReadFile(src)
	require.NoError(t, err)

	require.NoError(t, ArchiveNote(layout, "stale-fact"))

	_, statErr := os.Stat(layout.NotePath("stale-fact"))
	require.True(t, os.IsNotExist(statErr), "the live note must be gone after archiving")

	archived, err := os.ReadFile(filepath.Join(layout.ArchiveDir(), "stale-fact.md"))
	require.NoError(t, err, "the note must exist in the archive")
	require.Equal(t, original, archived, "archiving must not alter content")

	names, err := ListArchivedNotes(layout)
	require.NoError(t, err)
	require.Equal(t, []string{"stale-fact"}, names)

	require.NoError(t, RestoreNote(layout, "stale-fact"))

	restored, err := os.ReadFile(layout.NotePath("stale-fact"))
	require.NoError(t, err)
	require.Equal(t, original, restored)

	names, err = ListArchivedNotes(layout)
	require.NoError(t, err)
	require.Empty(t, names)
}

// TestArchiveNote_NeverClobbers covers the never-delete invariant at the
// edge: when the archive already holds a file of the same name, the live
// note stays where it is and both copies survive untouched.
func TestArchiveNote_NeverClobbers(t *testing.T) {
	layout := NewLayout(t.TempDir())
	require.NoError(t, EnsureLayout(layout))
	writeNote(t, layout.NotePath("stale-fact"), archiveTestNote)
	writeNote(t, filepath.Join(layout.ArchiveDir(), "stale-fact.md"), "archived long ago\n")

	err := ArchiveNote(layout, "stale-fact")

	require.Error(t, err)
	live, readErr := os.ReadFile(layout.NotePath("stale-fact"))
	require.NoError(t, readErr, "the live note must survive a refused archival")
	require.Equal(t, archiveTestNote, string(live))
	kept, readErr := os.ReadFile(filepath.Join(layout.ArchiveDir(), "stale-fact.md"))
	require.NoError(t, readErr)
	require.Equal(t, "archived long ago\n", string(kept), "the archived copy must survive untouched")
}

// TestRestoreNote_NeverClobbers covers the mirror edge: restoring onto a
// live note of the same name is refused and nothing changes.
func TestRestoreNote_NeverClobbers(t *testing.T) {
	layout := NewLayout(t.TempDir())
	require.NoError(t, EnsureLayout(layout))
	writeNote(t, layout.NotePath("stale-fact"), "a newer live note\n")
	writeNote(t, filepath.Join(layout.ArchiveDir(), "stale-fact.md"), archiveTestNote)

	err := RestoreNote(layout, "stale-fact")

	require.Error(t, err)
	live, readErr := os.ReadFile(layout.NotePath("stale-fact"))
	require.NoError(t, readErr)
	require.Equal(t, "a newer live note\n", string(live))
	kept, readErr := os.ReadFile(filepath.Join(layout.ArchiveDir(), "stale-fact.md"))
	require.NoError(t, readErr)
	require.Equal(t, archiveTestNote, string(kept))
}

// TestArchiveNote_Missing covers the error paths: archiving or restoring a
// note that does not exist fails with a readable error, not a panic.
func TestArchiveNote_Missing(t *testing.T) {
	layout := NewLayout(t.TempDir())
	require.NoError(t, EnsureLayout(layout))

	require.Error(t, ArchiveNote(layout, "no-such-note"))
	require.Error(t, RestoreNote(layout, "no-such-note"))
}

// TestListArchivedNotes_EmptyAndOdd covers the listing: a missing archive
// directory reads as empty, and non-note files are ignored.
func TestListArchivedNotes_EmptyAndOdd(t *testing.T) {
	layout := NewLayout(t.TempDir())

	names, err := ListArchivedNotes(layout)
	require.NoError(t, err)
	require.Empty(t, names, "a missing archive directory is an empty archive")

	require.NoError(t, EnsureLayout(layout))
	writeNote(t, filepath.Join(layout.ArchiveDir(), "a.md"), "a")
	writeNote(t, filepath.Join(layout.ArchiveDir(), "notes.txt"), "not a note")

	names, err = ListArchivedNotes(layout)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, names)
}
