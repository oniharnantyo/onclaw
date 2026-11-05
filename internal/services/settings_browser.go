package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/browseruse"
)

// The Browser Use methods on SettingsService: the settings pane's config
// read/save plus profile management (rebrand-browser-use-system-mcp, spec
// browser-use "Profile import" and the settings delta's Browser Use pane).
// The config persists as the server's own config JSON under the browser key
// in tool_settings — the same document the system server's config provider
// decodes at every spawn boundary (browseruse.DecodeStoredConfig, design
// D8). Profiles are filesystem-backed (design D3): the profile registry is
// the directory listing under <browserDir>/profiles.

// BrowserSettingsKey is the tool_settings key the Browser Use configuration
// persists under. It is the retired browser tool's storage key, kept
// verbatim: the stored document outlives the tool that named it, and the
// system server's config provider reads the same key.
const BrowserSettingsKey = "browser"

// Browser save-time limits: a timeout of zero would disable the idle reaper
// or hang actions forever, and an unbounded one would hide a wedged browser.
const (
	browserIdleTimeoutMax   = 3600 // one hour
	browserActionTimeoutMax = 600  // ten minutes
)

// BrowserConfig is the persisted Browser Use configuration, as bound to the
// frontend for the Browser Use settings pane. The JSON field names are the
// stored document's — the same names the system server's config provider
// decodes (browseruse.DecodeStoredConfig) — so this is one document,
// surfaced for editing.
type BrowserConfig struct {
	// DefaultProfile is the profile actions run in when the model passes
	// none. Empty means the default profile
	// (browseruse.DefaultBrowserProfile).
	DefaultProfile string `json:"defaultProfile"`
	// Headless launches Chromium without a window; the default is on.
	Headless bool `json:"headless"`
	// IdleTimeoutSeconds closes a profile's idle browser after this long.
	IdleTimeoutSeconds int `json:"idleTimeoutSeconds"`
	// ActionTimeoutSeconds bounds each browser action.
	ActionTimeoutSeconds int `json:"actionTimeoutSeconds"`
	// AllowPrivate permits navigation to loopback/private network targets;
	// the default is off.
	AllowPrivate bool `json:"allowPrivate"`
}

// BrowserProfile is one named browser profile as the dialog's profile list
// renders it.
type BrowserProfile struct {
	// Name is the profile name; the model addresses it as the `profile`
	// argument on any browser action.
	Name string `json:"name"`
	// ModifiedAt is the profile directory's last modification time.
	ModifiedAt time.Time `json:"modified_at"`
	// SizeBytes is the total size of the profile's files.
	SizeBytes int64 `json:"size_bytes"`
}

// storedBrowserConfig is the tool_settings document's shape. Pointer fields
// separate a key that is missing (keep the default) from one stored
// explicitly (even at its zero value). It mirrors browseruse.storedConfigJSON
// — the single decode site for the spawned server — so this is one document,
// surfaced for editing.
type storedBrowserConfig struct {
	DefaultProfile       *string `json:"defaultProfile"`
	Headless             *bool   `json:"headless"`
	IdleTimeoutSeconds   *int    `json:"idleTimeoutSeconds"`
	ActionTimeoutSeconds *int    `json:"actionTimeoutSeconds"`
	AllowPrivate         *bool   `json:"allowPrivate"`
}

// BrowserConfig returns the persisted Browser Use configuration. A missing
// row — or stored keys left out — falls back to the defaults, exactly as the
// system server's config provider resolves them (browseruse.DefaultConfig).
func (s *SettingsService) BrowserConfig(ctx context.Context) (BrowserConfig, error) {
	raw, ok, err := s.store.ToolSettings().Get(ctx, BrowserSettingsKey)
	if err != nil {
		return BrowserConfig{}, fmt.Errorf("get browser config: %w", err)
	}
	def := browseruse.DefaultConfig()
	cfg := BrowserConfig{
		DefaultProfile:       def.DefaultProfile,
		Headless:             def.Headless,
		IdleTimeoutSeconds:   def.IdleTimeoutSeconds,
		ActionTimeoutSeconds: def.ActionTimeoutSeconds,
		AllowPrivate:         def.AllowPrivate,
	}
	if !ok || raw == "" {
		return cfg, nil
	}
	var stored storedBrowserConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return BrowserConfig{}, fmt.Errorf("decode browser config: %w", err)
	}
	if stored.DefaultProfile != nil {
		cfg.DefaultProfile = *stored.DefaultProfile
	}
	if stored.Headless != nil {
		cfg.Headless = *stored.Headless
	}
	if stored.IdleTimeoutSeconds != nil {
		cfg.IdleTimeoutSeconds = *stored.IdleTimeoutSeconds
	}
	if stored.ActionTimeoutSeconds != nil {
		cfg.ActionTimeoutSeconds = *stored.ActionTimeoutSeconds
	}
	if stored.AllowPrivate != nil {
		cfg.AllowPrivate = *stored.AllowPrivate
	}
	return cfg, nil
}

// SaveBrowserConfig create-or-replaces the Browser Use configuration. The
// default profile must be a valid profile name (empty means the default
// profile), the idle timeout must sit between 1 and 3600 seconds, and the
// action timeout between 1 and 600 seconds.
//
// The change takes effect at the server's next spawn boundary (design D8):
// the supervisor fetches this document fresh on every supervision tick and
// at every spawn, and restarts the child at the next idle boundary — never
// mid-action — so no separate notification is needed.
func (s *SettingsService) SaveBrowserConfig(ctx context.Context, cfg BrowserConfig) error {
	name := cfg.DefaultProfile
	if name == "" {
		name = browseruse.DefaultBrowserProfile
	}
	if err := browseruse.ValidateBrowserProfile(name); err != nil {
		return fmt.Errorf("save browser config: %w", err)
	}
	if cfg.IdleTimeoutSeconds < 1 || cfg.IdleTimeoutSeconds > browserIdleTimeoutMax {
		return fmt.Errorf("save browser config: idle timeout must be between 1 and %d seconds, got %d",
			browserIdleTimeoutMax, cfg.IdleTimeoutSeconds)
	}
	if cfg.ActionTimeoutSeconds < 1 || cfg.ActionTimeoutSeconds > browserActionTimeoutMax {
		return fmt.Errorf("save browser config: action timeout must be between 1 and %d seconds, got %d",
			browserActionTimeoutMax, cfg.ActionTimeoutSeconds)
	}
	stored := BrowserConfig{
		DefaultProfile:       name,
		Headless:             cfg.Headless,
		IdleTimeoutSeconds:   cfg.IdleTimeoutSeconds,
		ActionTimeoutSeconds: cfg.ActionTimeoutSeconds,
		AllowPrivate:         cfg.AllowPrivate,
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode browser config: %w", err)
	}
	if err := s.store.ToolSettings().Set(ctx, BrowserSettingsKey, string(raw)); err != nil {
		return fmt.Errorf("save browser config: %w", err)
	}
	return nil
}

// BrowserProfiles lists the browser profiles sorted by name. The default
// profile is always listed — even before its directory exists (first use
// creates it, spec browser-use "Persistent browser profiles"). A profile's
// size is the sum of its files' sizes; unreadable entries are skipped so the
// listing stays usable. The result is never nil.
func (s *SettingsService) BrowserProfiles(_ context.Context) ([]BrowserProfile, error) {
	dir, err := s.browserProfilesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list browser profiles: %w", err)
	}
	profiles := []BrowserProfile{{Name: browseruse.DefaultBrowserProfile}}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == browseruse.DefaultBrowserProfile {
			continue // the default profile is seeded above
		}
		info, err := entry.Info()
		if err != nil {
			continue // unreadable entry: skip, the listing stays usable
		}
		profiles = append(profiles, BrowserProfile{
			Name:       entry.Name(),
			ModifiedAt: info.ModTime().UTC(),
			SizeBytes:  browserDirSize(filepath.Join(dir, entry.Name())),
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// CreateBrowserProfile creates a profile's directory (mode 0700). An error
// naming the profile when it already exists. The spawned server discovers
// profiles on demand — no runtime call is needed.
func (s *SettingsService) CreateBrowserProfile(_ context.Context, name string) error {
	if err := browseruse.ValidateBrowserProfile(name); err != nil {
		return fmt.Errorf("create browser profile: %w", err)
	}
	dir, err := s.browserProfilesDir()
	if err != nil {
		return err
	}
	profileDir := filepath.Join(dir, name)
	if _, err := os.Stat(profileDir); err == nil {
		return fmt.Errorf("create browser profile: profile %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("create browser profile: stat %s: %w", profileDir, err)
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return fmt.Errorf("create browser profile: %w", err)
	}
	return nil
}

// DeleteBrowserProfile removes a profile. When the system server is live,
// its child is torn down first (supervisor restart, re-armed): the child's
// stdin-close shutdown exports the cookie jar and closes every profile
// browser, so the directory is removed only after its browser has closed
// (spec browser-use "Persistent browser profiles"; the settings delta's
// "deleting a profile SHALL remove it after its browser closes"). A
// not-running (errored or still starting) server owns no browser; the
// directory is removed directly. Deletion also — when the persisted config
// named the profile the default — resets the config's default profile to
// "default". The default profile itself cannot be deleted. Deleting an
// unknown profile is a no-op: the end state (no such profile) already holds.
func (s *SettingsService) DeleteBrowserProfile(ctx context.Context, name string) error {
	if name == browseruse.DefaultBrowserProfile {
		return errors.New("the default profile cannot be deleted")
	}
	if err := browseruse.ValidateBrowserProfile(name); err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	profileDir, err := browseruse.ProfileDir(s.browserDir, name)
	if err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	if s.systemServerRestart != nil && s.systemServerState != nil {
		if state, ok := s.systemServerState(); ok && state.Status == mcp.StatusConnected {
			s.systemServerRestart() // closes the child (and with it the profile's browser)
		}
	}
	if err := os.RemoveAll(profileDir); err != nil {
		return fmt.Errorf("delete browser profile %s: %w", name, err)
	}
	cfg, err := s.BrowserConfig(ctx)
	if err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	if cfg.DefaultProfile == name {
		cfg.DefaultProfile = browseruse.DefaultBrowserProfile
		if err := s.SaveBrowserConfig(ctx, cfg); err != nil {
			return fmt.Errorf("delete browser profile: %w", err)
		}
	}
	return nil
}

// ImportBrowserProfile copies an existing Chrome/Chromium profile directory
// into the profiles registry (spec browser-use "Profile import"): the
// source's contents are copied — never moved — into <browserDir>/profiles/,
// the name is validated before any filesystem access, and a collision
// appends a disambiguating suffix. The returned finalName is the registry
// name the data actually landed under; the source directory is left
// untouched either way.
func (s *SettingsService) ImportBrowserProfile(sourcePath, name string) (finalName string, err error) {
	finalName, err = browseruse.ImportProfile(sourcePath, filepath.Join(s.browserDir, "profiles"), name)
	if err != nil {
		return "", fmt.Errorf("import browser profile: %w", err)
	}
	return finalName, nil
}

// browserProfilesDir returns <browserDir>/profiles, creating it on demand:
// the dialog needs the parent to exist to list and create profiles.
func (s *SettingsService) browserProfilesDir() (string, error) {
	dir := filepath.Join(s.browserDir, "profiles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create profiles directory: %w", err)
	}
	return dir, nil
}

// browserDirSize sums a directory tree's file sizes. Unreadable files and
// subdirectories are skipped: the size is informational, never load-bearing.
func browserDirSize(dir string) int64 {
	var total int64
	// The walk function never fails, so WalkDir cannot return an error.
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
