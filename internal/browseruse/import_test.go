package browseruse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTree creates files under dir (mkdir -p semantics) for import fixtures.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	}))
	return out
}

func TestImportProfile(t *testing.T) {
	t.Run("copies contents and lists as a profile", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "chrome-profile")
		writeTree(t, source, map[string]string{
			"Default/Preferences":       `{"session":{"restore_on_startup":1}}`,
			"Default/Cookies":           "cookies-db",
			"Default/Extension Rules/1": "rules",
		})
		profilesRoot := filepath.Join(t.TempDir(), "profiles")

		final, err := ImportProfile(source, profilesRoot, "work")
		require.NoError(t, err)
		require.Equal(t, "work", final)

		imported := readTree(t, filepath.Join(profilesRoot, "work"))
		sourceFiles := readTree(t, source)
		require.Equal(t, sourceFiles, imported, "the copy must match the source")

		dir, err := os.Stat(filepath.Join(profilesRoot, "work"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0), dir.Mode().Perm()&0o077, "imported profiles stay private to the user")

		_, err = ProfileDir(profilesRoot, final)
		require.NoError(t, err, "the imported name must resolve as a profile")
	})

	t.Run("invalid name refused before any filesystem access", func(t *testing.T) {
		final, err := ImportProfile("/does/not/matter", "/does/not/matter-either", "../escape")
		require.ErrorContains(t, err, "invalid profile name")
		require.Equal(t, "", final)
	})

	t.Run("missing source is an error", func(t *testing.T) {
		_, err := ImportProfile(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "profiles"), "work")
		require.Error(t, err)
	})

	t.Run("collision appends a suffix and never overwrites", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "src")
		writeTree(t, source, map[string]string{"data.txt": "second"})
		profilesRoot := t.TempDir()
		writeTree(t, filepath.Join(profilesRoot, "work"), map[string]string{"data.txt": "first"})

		final, err := ImportProfile(source, profilesRoot, "work")
		require.NoError(t, err)
		require.Equal(t, "work-2", final)

		b, err := os.ReadFile(filepath.Join(profilesRoot, "work", "data.txt"))
		require.NoError(t, err)
		require.Equal(t, "first", string(b), "the existing profile must be untouched")
		b, err = os.ReadFile(filepath.Join(profilesRoot, final, "data.txt"))
		require.NoError(t, err)
		require.Equal(t, "second", string(b))
	})

	t.Run("collision suffix keeps names within the profile contract", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "src")
		writeTree(t, source, map[string]string{"data.txt": "x"})
		profilesRoot := t.TempDir()
		long := "a0123456789012345678901234567890" // exactly 32 chars
		writeTree(t, filepath.Join(profilesRoot, long), map[string]string{"data.txt": "x"})

		final, err := ImportProfile(source, profilesRoot, long)
		require.NoError(t, err)
		require.NotEqual(t, long, final)
		require.NoError(t, ValidateBrowserProfile(final), "suffixed names must still satisfy the contract")
	})

	t.Run("source stays untouched", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "src")
		writeTree(t, source, map[string]string{"Default/Preferences": "prefs", "Cookies": "db"})
		before := readTree(t, source)

		profilesRoot := t.TempDir()
		_, err := ImportProfile(source, profilesRoot, "work")
		require.NoError(t, err)
		require.Equal(t, before, readTree(t, source), "import is a copy: the source must not change")
	})

	t.Run("failure cleans up the half-written target", func(t *testing.T) {
		source := t.TempDir()
		writeTree(t, source, map[string]string{"ok.txt": "fine", "bad/secret.txt": "locked"})
		// An unreadable file fails the copy partway through.
		require.NoError(t, os.Chmod(filepath.Join(source, "bad", "secret.txt"), 0o000))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(source, "bad", "secret.txt"), 0o600) })

		profilesRoot := t.TempDir()
		_, err := ImportProfile(source, profilesRoot, "work")
		require.Error(t, err, "an unreadable source file must fail the import")

		_, statErr := os.Stat(filepath.Join(profilesRoot, "work"))
		require.True(t, os.IsNotExist(statErr), "the half-written target must be removed")
	})
}
