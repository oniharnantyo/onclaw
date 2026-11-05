package browseruse

import (
	"fmt"
	"path/filepath"
	"regexp"
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

// ProfileDir maps a validated profile name to its persistent user-data
// directory under browserDir.
func ProfileDir(browserDir, name string) (string, error) {
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
