package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// newDetectSettings builds a settings service whose Chrome user-data seam
// points at dir (which may not exist — the absent-Chrome case), with an
// isolated browser dir so import tests never share registry state.
func newDetectSettings(t *testing.T, dir string) *SettingsService {
	t.Helper()
	svc := NewSettingsService(fake.New(), WithBrowserDir(t.TempDir()))
	svc.chromeUserDataDir = func() (string, error) { return dir, nil }
	return svc
}

// writeChromeFixture lays out a fake Chrome user-data directory: a `Local
// State` naming the profiles' display names plus their directories.
func writeChromeFixture(t *testing.T, dir string, localState string, profileDirs ...string) {
	t.Helper()
	if localState != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Local State"), []byte(localState), 0o600))
	}
	for _, name := range profileDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "marker"), []byte("x"), 0o600))
	}
}

const fullLocalState = `{
  "profile": {
    "info_cache": {
      "Default": {"name": "Personal"},
      "Profile 1": {"name": "Work"},
      "Profile 2": {"name": ""}
    }
  }
}`

// TestDetectChromeProfiles_NamesOrderAndDefaultFirst covers the happy path:
// display names come from the info cache, an unnamed profile falls back to
// its directory name, `Default` sorts first, and sizes reflect the files.
func TestDetectChromeProfiles_NamesOrderAndDefaultFirst(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, fullLocalState, "Default", "Profile 1", "Profile 2")

	svc := newDetectSettings(t, dir)
	got, err := svc.DetectChromeProfiles()
	require.NoError(t, err)
	require.Equal(t, []ChromeProfileCandidate{
		{DirName: "Default", DisplayName: "Personal", IsDefault: true, SizeBytes: int64(len("x"))},
		{DirName: "Profile 1", DisplayName: "Work", IsDefault: false, SizeBytes: int64(len("x"))},
		{DirName: "Profile 2", DisplayName: "Profile 2", IsDefault: false, SizeBytes: int64(len("x"))},
	}, got)
}

// TestDetectChromeProfiles_FiltersStaleCacheEntries proves the
// existing-directory filter: a cache entry whose directory is gone is not
// listed, and non-profile directories (not in the cache) are skipped.
func TestDetectChromeProfiles_FiltersStaleCacheEntries(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, `{"profile":{"info_cache":{
		"Default":{"name":"Personal"},
		"Profile 9":{"name":"Ghost"}
	}}}`, "Default", "GrShaderCache")

	svc := newDetectSettings(t, dir)
	got, err := svc.DetectChromeProfiles()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Default", got[0].DirName)
}

// TestDetectChromeProfiles_DefaultListedWithoutCacheEntry proves the
// `Default` directory is listed even when the cache does not name it yet
// (a fresh install before Chrome records it).
func TestDetectChromeProfiles_DefaultListedWithoutCacheEntry(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, `{"profile":{"info_cache":{}}}`, "Default")

	svc := newDetectSettings(t, dir)
	got, err := svc.DetectChromeProfiles()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Default", got[0].DirName)
	require.Equal(t, "Default", got[0].DisplayName)
	require.True(t, got[0].IsDefault)
}

// TestDetectChromeProfiles_AbsentChromeIsEmpty proves Chrome's absence is an
// empty list, not an error — the pane renders the no-Chrome hint.
func TestDetectChromeProfiles_AbsentChromeIsEmpty(t *testing.T) {
	svc := newDetectSettings(t, filepath.Join(t.TempDir(), "no-chrome"))

	got, err := svc.DetectChromeProfiles()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

// TestDetectChromeProfiles_CorruptLocalStateErrors proves a corrupt `Local
// State` is a wrapped error, never a silent empty list.
func TestDetectChromeProfiles_CorruptLocalStateErrors(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, "{not json", "Default")

	svc := newDetectSettings(t, dir)
	_, err := svc.DetectChromeProfiles()
	require.Error(t, err)
	require.ErrorContains(t, err, "decode Local State")
}

// TestDefaultChromeUserDataDir_GOOSPaths proves the GOOS resolver derives
// each platform's path from the environment (design D9), switching via
// runtime.GOOS on this host.
func TestDefaultChromeUserDataDir(t *testing.T) {
	dir, err := defaultChromeUserDataDir()
	require.NoError(t, err)
	require.NotEmpty(t, dir)
	require.True(t, filepath.IsAbs(dir))
}

// TestImportChromeProfile derives the registry name from the display name,
// copies the source under it, and leaves the source untouched.
func TestImportChromeProfile_ImportsUnderDerivedName(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, fullLocalState, "Default", "Profile 1")

	svc := newDetectSettings(t, dir)
	final, err := svc.ImportChromeProfile("Profile 1")
	require.NoError(t, err)
	require.Equal(t, "work", final)

	profiles := svc.browserDir + "/profiles"
	require.DirExists(t, filepath.Join(profiles, "work"))
	require.FileExists(t, filepath.Join(profiles, "work", "marker"))
	// The source is untouched.
	require.FileExists(t, filepath.Join(dir, "Profile 1", "marker"))
}

// TestImportChromeProfile_CollisionSuffixes proves a repeat import of the
// same candidate lands under a suffixed name (the backend collision rule).
func TestImportChromeProfile_CollisionSuffixes(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, fullLocalState, "Profile 1")

	svc := newDetectSettings(t, dir)
	first, err := svc.ImportChromeProfile("Profile 1")
	require.NoError(t, err)
	second, err := svc.ImportChromeProfile("Profile 1")
	require.NoError(t, err)
	require.Equal(t, "work", first)
	require.Equal(t, "work-2", second)
}

// TestImportChromeProfile_NameFallbacks covers the name ladder: an unnamed
// cache entry falls back to the lowercased directory name, and a directory
// name outside the contract falls back to "chrome".
func TestImportChromeProfile_NameFallbacks(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, `{"profile":{"info_cache":{"Profile 2":{"name":""}}}}`, "Profile 2", "Ünïcode", "日本語")
	// Chrome-made directories with non-contract names must still exist.
	require.DirExists(t, filepath.Join(dir, "Ünïcode"))
	require.DirExists(t, filepath.Join(dir, "日本語"))

	svc := newDetectSettings(t, dir)
	fromDir, err := svc.ImportChromeProfile("Profile 2")
	require.NoError(t, err)
	require.Equal(t, "profile-2", fromDir)

	partial, err := svc.ImportChromeProfile("Ünïcode")
	require.NoError(t, err)
	require.Equal(t, "ncode", partial) // the ASCII runes survive the slug

	last, err := svc.ImportChromeProfile("日本語")
	require.NoError(t, err)
	require.Equal(t, "chrome", last) // nothing survives: the last fallback
}

// TestImportChromeProfile_RejectsTraversal jails the directory name: no
// separators, no dot segments, no absolute paths.
func TestImportChromeProfile_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	writeChromeFixture(t, dir, fullLocalState, "Default")

	svc := newDetectSettings(t, dir)
	for _, bad := range []string{"../Default", "a/b", "/etc", "..", ".", ""} {
		_, err := svc.ImportChromeProfile(bad)
		require.Error(t, err, "dirName %q must be refused", bad)
	}
}

// TestChromeProfileNameSlug pins the slug rule the registry names derive
// from.
func TestChromeProfileNameSlug(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Work", "work"},
		{"Personal 2", "personal-2"},
		{"  Spaced  Name  ", "spaced-name"},
		{"Ünïcode", "ncode"},
		{"日本語", ""},
		{"Mi|xed", "mixed"},
		{"--deep--", "deep"},
		{"A Very Long Display Name That Overflows The Limit!", "a-very-long-display-name-that-ov"},
	} {
		require.Equal(t, tc.want, ChromeProfileNameSlug(tc.in), "slug %q", tc.in)
	}
}
