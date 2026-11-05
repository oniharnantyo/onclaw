package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/browseruse"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// newBrowserSettings builds a settings service over a fake store whose
// browser directory roots at a fresh temporary directory.
func newBrowserSettings(t *testing.T) (*SettingsService, *fake.Store, string) {
	t.Helper()
	fs := fake.New()
	dir := t.TempDir()
	return NewSettingsService(fs, WithBrowserDir(dir)), fs, dir
}

// TestSettingsBrowser_ConfigDefaultsWhenUnset proves a fresh store yields
// the defaults — the same values the system server's config provider
// resolves at spawn (browseruse.DefaultConfig).
func TestSettingsBrowser_ConfigDefaultsWhenUnset(t *testing.T) {
	svc, _, _ := newBrowserSettings(t)

	cfg, err := svc.BrowserConfig(context.Background())
	require.NoError(t, err)
	def := browseruse.DefaultConfig()
	require.Equal(t, BrowserConfig{
		DefaultProfile:       def.DefaultProfile,
		Headless:             def.Headless,
		IdleTimeoutSeconds:   def.IdleTimeoutSeconds,
		ActionTimeoutSeconds: def.ActionTimeoutSeconds,
		AllowPrivate:         def.AllowPrivate,
	}, cfg)
}

// TestSettingsBrowser_ConfigRoundTrip saves a config and reads it back,
// proving the persisted JSON carries the exact keys the runtime's config
// source decodes — an explicitly stored headless:false stays false.
func TestSettingsBrowser_ConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, fs, _ := newBrowserSettings(t)

	require.NoError(t, svc.SaveBrowserConfig(ctx, BrowserConfig{
		DefaultProfile:       "work",
		Headless:             false,
		IdleTimeoutSeconds:   300,
		ActionTimeoutSeconds: 45,
		AllowPrivate:         true,
	}))

	raw, ok, err := fs.ToolSettings().Get(ctx, BrowserSettingsKey)
	require.NoError(t, err)
	require.True(t, ok)
	var stored storedBrowserConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.NotNil(t, stored.DefaultProfile)
	require.Equal(t, "work", *stored.DefaultProfile)
	require.NotNil(t, stored.Headless)
	require.False(t, *stored.Headless)
	require.NotNil(t, stored.IdleTimeoutSeconds)
	require.Equal(t, 300, *stored.IdleTimeoutSeconds)
	require.NotNil(t, stored.ActionTimeoutSeconds)
	require.Equal(t, 45, *stored.ActionTimeoutSeconds)
	require.NotNil(t, stored.AllowPrivate)
	require.True(t, *stored.AllowPrivate)

	cfg, err := svc.BrowserConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, BrowserConfig{
		DefaultProfile:       "work",
		Headless:             false,
		IdleTimeoutSeconds:   300,
		ActionTimeoutSeconds: 45,
		AllowPrivate:         true,
	}, cfg)

	// An empty default profile saves as the default profile.
	require.NoError(t, svc.SaveBrowserConfig(ctx, BrowserConfig{
		Headless:             true,
		IdleTimeoutSeconds:   120,
		ActionTimeoutSeconds: 30,
	}))
	cfg, err = svc.BrowserConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, browseruse.DefaultBrowserProfile, cfg.DefaultProfile)
}

// TestSettingsBrowser_SaveRejectsInvalid covers the save validation: the
// default profile must be a valid profile name and both timeouts must sit
// inside their bounds.
func TestSettingsBrowser_SaveRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		cfg  BrowserConfig
		want string
	}{
		{"bad profile name upper", BrowserConfig{DefaultProfile: "Work", IdleTimeoutSeconds: 120, ActionTimeoutSeconds: 30}, "invalid profile name"},
		{"bad profile name traversal", BrowserConfig{DefaultProfile: "../etc", IdleTimeoutSeconds: 120, ActionTimeoutSeconds: 30}, "invalid profile name"},
		{"zero idle timeout", BrowserConfig{IdleTimeoutSeconds: 0, ActionTimeoutSeconds: 30}, "idle timeout"},
		{"oversized idle timeout", BrowserConfig{IdleTimeoutSeconds: 3601, ActionTimeoutSeconds: 30}, "idle timeout"},
		{"zero action timeout", BrowserConfig{IdleTimeoutSeconds: 120, ActionTimeoutSeconds: 0}, "action timeout"},
		{"oversized action timeout", BrowserConfig{IdleTimeoutSeconds: 120, ActionTimeoutSeconds: 601}, "action timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newBrowserSettings(t)
			err := svc.SaveBrowserConfig(context.Background(), tt.cfg)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// TestSettingsBrowser_Profiles proves the listing is the filesystem
// registry: default is always present (even before its directory exists),
// created profiles appear sorted by name, and sizes sum the profile's files.
func TestSettingsBrowser_Profiles(t *testing.T) {
	ctx := context.Background()
	svc, _, dir := newBrowserSettings(t)

	profiles, err := svc.BrowserProfiles(ctx)
	require.NoError(t, err)
	require.Equal(t, []BrowserProfile{{Name: browseruse.DefaultBrowserProfile}}, profiles)

	require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))
	require.NoError(t, svc.CreateBrowserProfile(ctx, "alpha"))

	// A file inside a profile counts toward its size.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "profiles", "alpha", "cookie"),
		[]byte("1234567890"), 0o600))

	profiles, err = svc.BrowserProfiles(ctx)
	require.NoError(t, err)
	require.Len(t, profiles, 3)
	require.Equal(t, "alpha", profiles[0].Name)
	require.Equal(t, "default", profiles[1].Name)
	require.Equal(t, "work", profiles[2].Name)

	require.Equal(t, int64(10), profiles[0].SizeBytes)
	require.WithinDuration(t, time.Now().UTC(), profiles[0].ModifiedAt, 10*time.Second)

	// The default profile has no directory yet: zero timestamp and size.
	require.Equal(t, int64(0), profiles[1].SizeBytes)
	require.True(t, profiles[1].ModifiedAt.IsZero())

	// The profiles parent was created by the listing.
	info, err := os.Stat(filepath.Join(dir, "profiles"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

// TestSettingsBrowser_CreateDuplicate proves creating an existing profile is
// an error naming the profile.
func TestSettingsBrowser_CreateDuplicate(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newBrowserSettings(t)

	require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))
	err := svc.CreateBrowserProfile(ctx, "work")
	require.ErrorContains(t, err, `profile "work" already exists`)

	// Invalid names never touch the filesystem.
	require.Error(t, svc.CreateBrowserProfile(ctx, "Work"))
}

// TestSettingsBrowser_DeleteRefusesDefault proves the default profile is
// protected (spec add-browser-tool "Default profile is protected").
func TestSettingsBrowser_DeleteRefusesDefault(t *testing.T) {
	svc, _, _ := newBrowserSettings(t)
	err := svc.DeleteBrowserProfile(context.Background(), browseruse.DefaultBrowserProfile)
	require.ErrorContains(t, err, "the default profile cannot be deleted")
}

// TestSettingsBrowser_DeleteRemovesAndResets proves deletion removes the
// directory and, when the config named the deleted profile the default,
// resets the default to "default".
func TestSettingsBrowser_DeleteRemovesAndResets(t *testing.T) {
	ctx := context.Background()
	svc, _, dir := newBrowserSettings(t)

	require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))
	require.NoError(t, svc.SaveBrowserConfig(ctx, BrowserConfig{
		DefaultProfile:       "work",
		Headless:             true,
		IdleTimeoutSeconds:   120,
		ActionTimeoutSeconds: 30,
	}))

	require.NoError(t, svc.DeleteBrowserProfile(ctx, "work"))

	_, err := os.Stat(filepath.Join(dir, "profiles", "work"))
	require.True(t, os.IsNotExist(err), "the profile directory must be gone")

	profiles, err := svc.BrowserProfiles(ctx)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, browseruse.DefaultBrowserProfile, profiles[0].Name)

	cfg, err := svc.BrowserConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, browseruse.DefaultBrowserProfile, cfg.DefaultProfile)
}

// TestSettingsBrowser_DeleteUnknownIsNoOp proves deleting a profile that
// does not exist succeeds without creating anything: the end state (no such
// profile) already holds. Documented no-op, not an error.
func TestSettingsBrowser_DeleteUnknownIsNoOp(t *testing.T) {
	ctx := context.Background()
	svc, _, dir := newBrowserSettings(t)

	require.NoError(t, svc.DeleteBrowserProfile(ctx, "ghost"))

	_, err := os.Stat(filepath.Join(dir, "profiles", "ghost"))
	require.True(t, os.IsNotExist(err))
}

// TestSettingsBrowser_ImportBrowserProfile proves the import copies the
// source directory into the profiles registry under a validated name,
// leaves the source untouched, and suffixes a name collision instead of
// overwriting (spec browser-use "Profile import": import copies and lists,
// collision is suffixed, source stays intact).
func TestSettingsBrowser_ImportBrowserProfile(t *testing.T) {
	svc, _, dir := newBrowserSettings(t)

	source := filepath.Join(t.TempDir(), "chrome-user-data")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "Default"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "Default", "Cookies"), []byte("jar"), 0o600))

	final, err := svc.ImportBrowserProfile(source, "work")
	require.NoError(t, err)
	require.Equal(t, "work", final)

	copied, err := os.ReadFile(filepath.Join(dir, "profiles", "work", "Default", "Cookies"))
	require.NoError(t, err)
	require.Equal(t, "jar", string(copied))
	kept, err := os.ReadFile(filepath.Join(source, "Default", "Cookies"))
	require.NoError(t, err, "the source directory must stay intact")
	require.Equal(t, "jar", string(kept))

	// The imported profile lists like any other.
	profiles, err := svc.BrowserProfiles(context.Background())
	require.NoError(t, err)
	require.Len(t, profiles, 2) // default + work

	// A collision lands under a suffixed name instead of overwriting.
	final, err = svc.ImportBrowserProfile(source, "work")
	require.NoError(t, err)
	require.Equal(t, "work-2", final)

	// An invalid name is refused before any filesystem access.
	_, err = svc.ImportBrowserProfile(source, "Not Valid")
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(dir, "profiles", "Not Valid"))
	require.True(t, os.IsNotExist(err))
}

// TestSettingsBrowser_DeleteCoordinatesSystemServer proves the
// profile-delete flow honors "deleting a profile SHALL remove it after its
// browser closes": a live (connected) system server is restarted — the
// synchronous seam standing in for the child teardown that closes the
// profile's browser — before the directory goes, while a failed or absent
// server owns no browser and is left alone.
func TestSettingsBrowser_DeleteCoordinatesSystemServer(t *testing.T) {
	ctx := context.Background()

	newCoordinatedSettings := func(dir string, state mcp.ServerState, supervised bool) (svc *SettingsService, restarts *int) {
		fs := fake.New()
		count := 0
		svc = NewSettingsService(fs,
			WithBrowserDir(dir),
			WithSystemServerState(func() (mcp.ServerState, bool) { return state, supervised }),
			WithSystemServerRestart(func() { count++ }))
		return svc, &count
	}

	t.Run("connected server is restarted before removal", func(t *testing.T) {
		dir := t.TempDir()
		svc, restarts := newCoordinatedSettings(dir, mcp.ServerState{Status: mcp.StatusConnected}, true)
		require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))

		require.NoError(t, svc.DeleteBrowserProfile(ctx, "work"))
		require.Equal(t, 1, *restarts, "a live server's child must be torn down so the browser closes first")
		_, err := os.Stat(filepath.Join(dir, "profiles", "work"))
		require.True(t, os.IsNotExist(err), "the profile directory is removed after the browser closed")
	})

	t.Run("failed server is not restarted", func(t *testing.T) {
		dir := t.TempDir()
		svc, restarts := newCoordinatedSettings(dir,
			mcp.ServerState{Status: mcp.StatusError, StatusError: "spawn failed"}, true)
		require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))

		require.NoError(t, svc.DeleteBrowserProfile(ctx, "work"))
		require.Zero(t, *restarts, "a failed server owns no live browser; the directory is removed directly")
		_, err := os.Stat(filepath.Join(dir, "profiles", "work"))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("no supervised server is not restarted", func(t *testing.T) {
		dir := t.TempDir()
		svc, restarts := newCoordinatedSettings(dir, mcp.ServerState{}, false)
		require.NoError(t, svc.CreateBrowserProfile(ctx, "work"))

		require.NoError(t, svc.DeleteBrowserProfile(ctx, "work"))
		require.Zero(t, *restarts)
		_, err := os.Stat(filepath.Join(dir, "profiles", "work"))
		require.True(t, os.IsNotExist(err))
	})
}
