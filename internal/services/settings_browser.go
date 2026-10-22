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

	"github.com/oniharnantyo/buddies/internal/agents/tools"
)

// The browser tool methods on SettingsService: the Tools pane's Browser
// dialog — config read/save plus profile management (spec add-browser-tool
// D9/D10). The config persists as the runtime's own browser config JSON under
// tool key browser in tool_settings, so the tool's config source
// (agents.storeBrowserConfigSource) consumes exactly what this service
// writes. Profiles are filesystem-backed (design D3): the profile registry
// is the directory listing under <browserDir>/profiles.

// Browser save-time limits: a timeout of zero would disable the idle reaper
// or hang actions forever, and an unbounded one would hide a wedged browser.
const (
	browserIdleTimeoutMax   = 3600 // one hour
	browserActionTimeoutMax = 600  // ten minutes
)

// BrowserConfig is the persisted browser tool configuration, as bound to the
// frontend for the Tools pane's Browser dialog. The JSON field names are the
// stored document's — the same names the runtime's config source decodes
// (agents' browserConfigJSON) — so this is one document, surfaced for
// editing.
type BrowserConfig struct {
	// DefaultProfile is the profile actions run in when the model passes
	// none. Empty means the default profile (tools.DefaultBrowserProfile).
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
// explicitly (even at its zero value). It mirrors agents' browserConfigJSON —
// the two must stay in step; they cannot share the type across packages.
type storedBrowserConfig struct {
	DefaultProfile       *string `json:"defaultProfile"`
	Headless             *bool   `json:"headless"`
	IdleTimeoutSeconds   *int    `json:"idleTimeoutSeconds"`
	ActionTimeoutSeconds *int    `json:"actionTimeoutSeconds"`
	AllowPrivate         *bool   `json:"allowPrivate"`
}

// BrowserConfig returns the persisted browser tool configuration. A missing
// row — or stored keys left out — falls back to the defaults, exactly as the
// runtime's config source resolves them (spec add-browser-tool D9).
func (s *SettingsService) BrowserConfig(ctx context.Context) (BrowserConfig, error) {
	raw, ok, err := s.store.ToolSettings().Get(ctx, tools.NameBrowser)
	if err != nil {
		return BrowserConfig{}, fmt.Errorf("get browser config: %w", err)
	}
	def := tools.DefaultBrowserConfig()
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

// SaveBrowserConfig create-or-replaces the browser tool configuration. The
// default profile must be a valid profile name (empty means the default
// profile), the idle timeout must sit between 1 and 3600 seconds, and the
// action timeout between 1 and 600 seconds.
func (s *SettingsService) SaveBrowserConfig(ctx context.Context, cfg BrowserConfig) error {
	name := cfg.DefaultProfile
	if name == "" {
		name = tools.DefaultBrowserProfile
	}
	if err := tools.ValidateBrowserProfile(name); err != nil {
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
	if err := s.store.ToolSettings().Set(ctx, tools.NameBrowser, string(raw)); err != nil {
		return fmt.Errorf("save browser config: %w", err)
	}
	return nil
}

// BrowserProfiles lists the browser profiles sorted by name. The default
// profile is always listed — even before its directory exists (first use
// creates it, spec add-browser-tool D3). A profile's size is the sum of its
// files' sizes; unreadable entries are skipped so the listing stays usable.
// The result is never nil.
func (s *SettingsService) BrowserProfiles(_ context.Context) ([]BrowserProfile, error) {
	dir, err := s.browserProfilesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list browser profiles: %w", err)
	}
	profiles := []BrowserProfile{{Name: tools.DefaultBrowserProfile}}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == tools.DefaultBrowserProfile {
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
// naming the profile when it already exists.
func (s *SettingsService) CreateBrowserProfile(_ context.Context, name string) error {
	if err := tools.ValidateBrowserProfile(name); err != nil {
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

// DeleteBrowserProfile removes a profile: it closes the profile's live
// browser (if any), deletes its directory, and — when the persisted config
// named it the default profile — resets the config's default profile to
// "default". The default profile itself cannot be deleted. Deleting an
// unknown profile is a no-op: the end state (no such profile) already holds.
func (s *SettingsService) DeleteBrowserProfile(ctx context.Context, name string) error {
	if name == tools.DefaultBrowserProfile {
		return errors.New("the default profile cannot be deleted")
	}
	if err := tools.ValidateBrowserProfile(name); err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	profileDir, err := tools.BrowserProfileDir(s.browserDir, name)
	if err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	if err := tools.ShutdownBrowserProfile(name); err != nil {
		return fmt.Errorf("delete browser profile %s: %w", name, err)
	}
	if err := os.RemoveAll(profileDir); err != nil {
		return fmt.Errorf("delete browser profile %s: %w", name, err)
	}
	cfg, err := s.BrowserConfig(ctx)
	if err != nil {
		return fmt.Errorf("delete browser profile: %w", err)
	}
	if cfg.DefaultProfile == name {
		cfg.DefaultProfile = tools.DefaultBrowserProfile
		if err := s.SaveBrowserConfig(ctx, cfg); err != nil {
			return fmt.Errorf("delete browser profile: %w", err)
		}
	}
	return nil
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
