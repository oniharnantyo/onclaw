package browseruse

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultBrowserProfile is the profile used when neither the call nor the
// configuration names one; it is created automatically on first use and can
// never be deleted.
const DefaultBrowserProfile = "default"

// profilePattern is the profile-name contract: path-traversal-proof and safe
// to join under the profiles directory.
var profilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidateBrowserProfile reports whether name may be used as a browser
// profile. It performs no filesystem access.
func ValidateBrowserProfile(name string) error {
	if !profilePattern.MatchString(name) {
		return fmt.Errorf("browser: invalid profile name %q: must match ^[a-z0-9][a-z0-9_-]{0,31}$", name)
	}
	return nil
}

// ProfileDir maps a profile to its persistent user-data directory. A
// validated profile name maps under browserDir; an absolute-path name is a
// bound profile directory (the host-stamped sessions key, design D3 of
// add-per-agent-browser-profiles) and passes through cleaned and
// traversal-checked — so createSession, profileExists, and relaunchHeaded
// work on bound keys unchanged. Named keys keep the name contract.
func ProfileDir(browserDir, name string) (string, error) {
	if filepath.IsAbs(name) {
		cleaned := filepath.Clean(name)
		if !filepath.IsAbs(cleaned) || strings.Contains(cleaned, "..") {
			return "", fmt.Errorf("browser: invalid profile dir %q", name)
		}
		return cleaned, nil
	}
	if err := ValidateBrowserProfile(name); err != nil {
		return "", err
	}
	return filepath.Join(browserDir, "profiles", name), nil
}

// ResolveProfile applies the profile precedence: explicit argument,
// configured default, then DefaultBrowserProfile — validating before any
// filesystem access.
func ResolveProfile(arg, configured string) (string, error) {
	name := arg
	if name == "" {
		name = configured
	}
	if name == "" {
		name = DefaultBrowserProfile
	}
	if err := ValidateBrowserProfile(name); err != nil {
		return "", err
	}
	return name, nil
}
