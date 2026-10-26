package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolateHome points HOME at a fresh temporary directory so the layout under
// test never touches the real user home.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// TestResolve_PlacesLayoutUnderHome covers task 3.1 path resolution: the
// layout roots at <HOME>/.buddies with the database, skills, and logs paths
// derived from it.
func TestResolve_PlacesLayoutUnderHome(t *testing.T) {
	home := isolateHome(t)

	layout, err := Resolve()
	require.NoError(t, err)

	root := filepath.Join(home, ".buddies")
	require.Equal(t, root, layout.Root)
	require.Equal(t, filepath.Join(root, "buddies.db"), layout.DBPath)
	require.Equal(t, filepath.Join(root, "skills"), layout.Skills)
	require.Equal(t, filepath.Join(root, "workspaces"), layout.Workspaces)
	require.Equal(t, filepath.Join(root, "attachments"), layout.Attachments)
	require.Equal(t, filepath.Join(root, "knowledge"), layout.Knowledge)
	require.Equal(t, filepath.Join(root, "browser"), layout.Browser)
	require.Equal(t, filepath.Join(root, "logs"), layout.Logs)
}

// TestResolve_MissingHome errors when no home directory can be resolved
// (HOME unset) instead of silently writing somewhere unexpected.
func TestResolve_MissingHomeReturnsError(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := Resolve()
	require.Error(t, err)
}

// TestEnsure_FirstRunCreatesLayout covers task 3.1 first run: starting from
// nothing, every path of the layout exists afterwards and directories are
// private to the user (0700).
func TestEnsure_FirstRunCreatesLayout(t *testing.T) {
	isolateHome(t)

	layout, err := Resolve()
	require.NoError(t, err)
	require.NoError(t, layout.Ensure())

	for _, dir := range []string{layout.Root, layout.Skills, layout.Workspaces, layout.Attachments, layout.Knowledge, layout.Browser, layout.Logs} {
		info, err := os.Stat(dir)
		require.NoError(t, err, "directory %s must exist after first run", dir)
		require.True(t, info.IsDir(), "%s must be a directory", dir)
		require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "%s must be mode 0700", dir)
	}

	info, err := os.Stat(layout.DBPath)
	require.NoError(t, err, "database file must exist after first run")
	require.False(t, info.IsDir())
	require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "database file must be mode 0600")
}

// TestEnsure_SecondRunIsNoOp covers task 3.1 idempotency: a second Ensure on
// an existing layout succeeds, keeps permissions, and never touches content
// placed inside the layout.
func TestEnsure_SecondRunIsNoOp(t *testing.T) {
	isolateHome(t)

	layout, err := Resolve()
	require.NoError(t, err)
	require.NoError(t, layout.Ensure())

	skillFile := filepath.Join(layout.Skills, "my-skill.md")
	require.NoError(t, os.WriteFile(skillFile, []byte("# my skill"), 0600))

	require.NoError(t, layout.Ensure())

	_, err = os.Stat(skillFile)
	require.NoError(t, err, "existing content must survive a re-run")
	info, err := os.Stat(layout.Root)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())
}
