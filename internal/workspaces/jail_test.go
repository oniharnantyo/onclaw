package workspaces

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The jailed reader tests (task 8.1, design D8, spec chat-panels "Workspace
// reader stays jailed"): the reader serves workspace files by relative path
// and refuses everything that reaches outside the agent's workspace.

// seedWorkspace builds a manager over a temp root with a "scout" workspace
// holding a file and a subdirectory.
func seedWorkspace(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := NewManager(root)
	dir, err := m.Create("scout")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("you are scout"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, DirTasks), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, DirTasks, "task-1.md"), []byte("# task one"), 0600))
	return m, root
}

// TestReadFile_InWorkspaceWorks covers the happy path: a root-level file and
// a nested file read by workspace-relative path.
func TestReadFile_InWorkspaceWorks(t *testing.T) {
	m, _ := seedWorkspace(t)

	content, err := m.ReadFile("scout", "IDENTITY.md")
	require.NoError(t, err)
	require.Equal(t, "you are scout", content)

	content, err = m.ReadFile("scout", ".tasks/task-1.md")
	require.NoError(t, err)
	require.Equal(t, "# task one", content)

	// A clean relative form that stays inside resolves to the same file.
	content, err = m.ReadFile("scout", "./.tasks/task-1.md")
	require.NoError(t, err)
	require.Equal(t, "# task one", content)
}

// TestReadFile_RefusesTraversal pins the traversal refusals: any ".."
// element and any absolute path are refused before the open.
func TestReadFile_RefusesTraversal(t *testing.T) {
	m, root := seedWorkspace(t)

	for _, rel := range []string{
		"../escape.txt",
		"..",
		"tasks/../../../escape.txt",
		"/etc/passwd",
	} {
		_, err := m.ReadFile("scout", rel)
		require.Error(t, err, "%q must be refused", rel)
		require.Contains(t, err.Error(), "refused", "%q is refused as a jail break", rel)
	}

	// Defense in depth: nothing escaped onto disk outside the workspace.
	_, err := os.Stat(filepath.Join(root, "escape.txt"))
	require.Error(t, err, "no file was written outside the workspace")
}

// TestReadFile_RefusesSymlinkEscape pins the symlink rule: a symlink inside
// the workspace pointing out is refused before the open.
func TestReadFile_RefusesSymlinkEscape(t *testing.T) {
	m, root := seedWorkspace(t)

	// A foreign file outside the workspace and a symlink to it inside.
	foreign := filepath.Join(root, "foreign.md")
	require.NoError(t, os.WriteFile(foreign, []byte("secret"), 0600))
	require.NoError(t, os.Symlink(foreign, filepath.Join(m.Root(), "scout", "leak.md")))

	_, err := m.ReadFile("scout", "leak.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink", "the symlink escape is named as such")

	// A symlinked directory escape is refused the same way.
	require.NoError(t, os.Symlink(root, filepath.Join(m.Root(), "scout", "rootlink")))
	_, err = m.ReadFile("scout", filepath.Join("rootlink", "foreign.md"))
	require.Error(t, err)
}

// TestReadFile_MissingFileIsNotFound pins the clean absent state.
func TestReadFile_MissingFileIsNotFound(t *testing.T) {
	m, _ := seedWorkspace(t)

	_, err := m.ReadFile("scout", "absent.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	_, err = m.ReadFile("scout", "no/such/dir/file.md")
	require.Error(t, err)
}

// TestReadFile_RefusesRootAndUnknownSlug pins the remaining refusals: the
// workspace root itself is not a file, and an invalid slug fails like Dir.
func TestReadFile_RefusesRootAndUnknownSlug(t *testing.T) {
	m, _ := seedWorkspace(t)

	_, err := m.ReadFile("scout", "")
	require.Error(t, err, "the root is not a readable file")

	_, err = m.ReadFile("ghost", "IDENTITY.md")
	require.Error(t, err, "an absent workspace is refused")
}

// TestListDir_ListsAndJails covers the listing path: the root lists, a
// subdirectory lists, traversal is refused like the read path.
func TestListDir_ListsAndJails(t *testing.T) {
	m, _ := seedWorkspace(t)

	entries, err := m.ListDir("scout", "")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, DirTasks, entries[0].Name)
	require.True(t, entries[0].Dir)
	require.Equal(t, "IDENTITY.md", entries[1].Name)
	require.False(t, entries[1].Dir)
	require.Equal(t, int64(len("you are scout")), entries[1].Size)

	tasks, err := m.ListDir("scout", DirTasks)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "task-1.md", tasks[0].Name)

	for _, rel := range []string{"../", "/etc", "nope-absent"} {
		_, err := m.ListDir("scout", rel)
		if rel == "nope-absent" {
			require.ErrorContains(t, err, "not found")
			continue
		}
		require.Error(t, err, "%q must be refused", rel)
	}
}
