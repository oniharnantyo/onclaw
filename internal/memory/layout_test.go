package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// snapshotTree lists every path under root (slash-relative, sorted), so an
// idempotency test can compare the tree before and after a re-run.
func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	require.NoError(t, err)
	return paths
}

// TestEnsureLayout_CreatesTree covers first run: from a bare workspace, the
// memory directory and its archive subdirectory exist afterwards, private to
// the user (0700).
func TestEnsureLayout_CreatesTree(t *testing.T) {
	workspace := t.TempDir()
	layout := NewLayout(workspace)

	require.NoError(t, EnsureLayout(layout))

	for _, dir := range []string{layout.Dir, layout.ArchiveDir()} {
		info, err := os.Stat(dir)
		require.NoError(t, err, "directory %s must exist after EnsureLayout", dir)
		require.True(t, info.IsDir(), "%s must be a directory", dir)
		require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "%s must be mode 0700", dir)
	}
	require.Equal(t, filepath.Join(workspace, DirName), layout.Dir)
	require.Equal(t, filepath.Join(layout.Dir, ArchiveDirName), layout.ArchiveDir())
}

// TestEnsureLayout_RunTwiceLeavesTreeUnchanged covers the idempotency
// contract: a second EnsureLayout succeeds and changes no path, no
// permission, and no content already inside the tree.
func TestEnsureLayout_RunTwiceLeavesTreeUnchanged(t *testing.T) {
	workspace := t.TempDir()
	layout := NewLayout(workspace)

	require.NoError(t, EnsureLayout(layout))

	// Content placed inside the tree between runs must survive untouched.
	require.NoError(t, os.WriteFile(layout.IndexPath(), []byte("# Memory\n"), filePerm))
	require.NoError(t, os.WriteFile(layout.NotePath("prefers-dark-mode.md"), []byte("---\nname: prefers-dark-mode\n---\n"), filePerm))
	require.NoError(t, os.WriteFile(filepath.Join(layout.ArchiveDir(), "old.md"), []byte("archived"), filePerm))

	before := snapshotTree(t, layout.Dir)
	require.NoError(t, EnsureLayout(layout))
	after := snapshotTree(t, layout.Dir)

	require.Equal(t, before, after, "a second EnsureLayout must leave the tree unchanged")

	info, err := os.Stat(layout.Dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "directory mode must survive a re-run")
}
