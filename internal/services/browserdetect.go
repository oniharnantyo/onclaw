package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Chrome profile detection for the Browser Use pane's import flow
// (add-browser-login-handover design D9, spec settings "Browser Use pane"):
// this machine's Google Chrome profiles, read from the GOOS-aware user-data
// directory's `Local State` profile cache. Chrome only, informational sizes,
// `Default` first — the pane's import list renders exactly this.

// ChromeProfileCandidate is one detected Chrome profile the pane's import
// list renders. It carries no source path the user could edit — Import
// resolves the source from DirName against this machine's Chrome
// installation (design D9: the manual source-path input is dropped).
type ChromeProfileCandidate struct {
	// DirName is the profile's directory name under the Chrome user-data
	// directory ("Default", "Profile 1", ...).
	DirName string `json:"dir_name"`
	// DisplayName is Chrome's own name for the profile ("Personal", ...);
	// the directory name when the cache names none.
	DisplayName string `json:"display_name"`
	// IsDefault marks the profile Chrome launches with ("Default").
	IsDefault bool `json:"is_default"`
	// SizeBytes is the profile directory's total file size — informational,
	// best-effort; zero when it cannot be measured.
	SizeBytes int64 `json:"size_bytes"`
}

// localState is the slice of Chrome's `Local State` document the detector
// reads: the per-profile display-name cache.
type localState struct {
	Profile struct {
		InfoCache map[string]struct {
			Name string `json:"name"`
		} `json:"info_cache"`
	} `json:"profile"`
}

// defaultChromeUserDataDir resolves this platform's Chrome user-data
// directory from the environment — never one hardcoded platform (design D9):
// darwin the Library path, Windows under %LOCALAPPDATA%, Linux the XDG
// config dir (falling back to ~/.config). An unresolvable home directory is
// an error; Chrome's absence is NOT — that returns the path, and Detect
// answers an empty list.
func defaultChromeUserDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect chrome profiles: resolve home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "Google", "Chrome", "User Data"), nil
	default: // linux and the rest: XDG_CONFIG_HOME, falling back to ~/.config
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		return filepath.Join(config, "google-chrome"), nil
	}
}

// DetectChromeProfiles lists this machine's Google Chrome profiles, `Default`
// first then the rest by directory name. Chrome absent (no user-data
// directory) is an empty list, not an error; a corrupt `Local State` is a
// wrapped error. Only candidates backed by an existing directory are listed
// — stale cache entries are skipped — and the `Default` directory is listed
// even when the cache does not name it yet. The result is never nil.
func (s *SettingsService) DetectChromeProfiles() ([]ChromeProfileCandidate, error) {
	userDataDir, err := s.chromeUserDataDir()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(userDataDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ChromeProfileCandidate{}, nil
		}
		return nil, fmt.Errorf("detect chrome profiles: stat %s: %w", userDataDir, err)
	}

	raw, err := os.ReadFile(filepath.Join(userDataDir, "Local State"))
	if err != nil {
		return nil, fmt.Errorf("detect chrome profiles: read Local State: %w", err)
	}
	var state localState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("detect chrome profiles: decode Local State: %w", err)
	}

	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return nil, fmt.Errorf("detect chrome profiles: read %s: %w", userDataDir, err)
	}
	candidates := make([]ChromeProfileCandidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		cached, known := state.Profile.InfoCache[name]
		if !known && name != "Default" {
			continue // existing-directory filter: only real profiles (and Default)
		}
		display := cached.Name
		if display == "" {
			display = name // dir-name fallback
		}
		candidates = append(candidates, ChromeProfileCandidate{
			DirName:     name,
			DisplayName: display,
			IsDefault:   name == "Default",
			SizeBytes:   browserDirSize(filepath.Join(userDataDir, name)),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].IsDefault != candidates[j].IsDefault {
			return candidates[i].IsDefault // `Default` first
		}
		return candidates[i].DirName < candidates[j].DirName
	})
	return candidates, nil
}

// ImportChromeProfile imports one detected Chrome profile in one click
// (spec settings "Browser Use pane"): the source directory is resolved from
// DirName against this machine's Chrome installation — the user never
// supplies a path (design D9: the manual source-path input is dropped). The
// registry name derives from the display name through the backend profile
// name rule, falling back to the lowercased directory name and then
// "chrome"; a collision suffixes (ImportBrowserProfile's engine). The
// returned finalName is the registry name the data landed under.
func (s *SettingsService) ImportChromeProfile(dirName string) (string, error) {
	userDataDir, err := s.chromeUserDataDir()
	if err != nil {
		return "", err
	}
	// Jail the directory name: a plain path element under the user-data
	// directory, never a traversal or an absolute path.
	if dirName == "" || dirName != filepath.Base(filepath.Clean(dirName)) || dirName == "." || dirName == ".." {
		return "", fmt.Errorf("import chrome profile: invalid profile directory %q", dirName)
	}
	source := filepath.Join(userDataDir, dirName)
	if _, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("import chrome profile: stat %s: %w", dirName, err)
	}
	display := chromeDisplayName(userDataDir, dirName)
	name := chromeRegistryName(display, dirName)
	finalName, err := s.ImportBrowserProfile(source, name)
	if err != nil {
		return "", err
	}
	return finalName, nil
}

// chromeDisplayName re-reads the profile's cached display name; the
// directory name when the cache names none or cannot be read (the detector
// already surfaced the friendly name — this only re-derives it for naming).
func chromeDisplayName(userDataDir, dirName string) string {
	raw, err := os.ReadFile(filepath.Join(userDataDir, "Local State"))
	if err != nil {
		return ""
	}
	var state localState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ""
	}
	return state.Profile.InfoCache[dirName].Name
}

// chromeRegistryName slugs a display name into the profile-name contract
// (^[a-z0-9][a-z0-9_-]{0,31}$): lowercase, spaces and separators fold to
// dashes, anything else invalid is stripped. The lowercased directory name
// is the first fallback, "chrome" the last.
func chromeRegistryName(display, dirName string) string {
	slug := ChromeProfileNameSlug(display)
	if slug == "" {
		slug = ChromeProfileNameSlug(dirName)
	}
	if slug == "" {
		slug = "chrome"
	}
	return slug
}

// ChromeProfileNameSlug folds a display name onto the backend profile-name
// contract: lowercase, runs of spaces/underscores/separators become single
// dashes, every rune outside [a-z0-9_-] is dropped, leading/trailing dashes
// are trimmed. Empty when nothing survives (the caller falls back).
func ChromeProfileNameSlug(name string) string {
	var b []byte
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-':
			if len(b) > 0 && (r == '-' || r == '_') && dash {
				continue // fold repeated separators
			}
			b = append(b, byte(r))
			dash = r == '-' || r == '_'
		case r == ' ':
			if len(b) > 0 && !dash {
				b = append(b, '-')
				dash = true
			}
		default:
			// dropped: accents, punctuation, anything outside the contract
		}
	}
	slug := strings.Trim(string(b), "-_")
	if len(slug) > 32 {
		slug = strings.TrimRight(slug[:32], "-_")
	}
	return slug
}
