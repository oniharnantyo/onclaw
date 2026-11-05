package browseruse

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ImportProfile copies an existing Chrome/Chromium profile directory into the
// profiles registry (design D9): the source's contents are COPIED — never
// moved — into profilesRoot/<name>/, and the source directory is left
// untouched. The name is validated before any filesystem access; a collision
// appends a disambiguating suffix ("-2", "-3", ...), shortening a 32-character
// name as needed so every produced name still matches the profile contract.
// A half-written target directory is removed on failure. The returned
// finalName is the registry name the data actually landed under.
func ImportProfile(sourceDir, profilesRoot, name string) (string, error) {
	// Validation first: an invalid name is refused before any filesystem
	// access happens.
	if err := ValidateBrowserProfile(name); err != nil {
		return "", err
	}
	info, err := os.Stat(sourceDir)
	if err != nil {
		return "", fmt.Errorf("browser: inspect import source: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("browser: import source %s is not a directory", sourceDir)
	}
	if err := os.MkdirAll(profilesRoot, 0o700); err != nil {
		return "", fmt.Errorf("browser: create profiles dir: %w", err)
	}
	final := freeProfileName(profilesRoot, name)
	target := filepath.Join(profilesRoot, final)
	if err := copyDirContents(sourceDir, target); err != nil {
		_ = os.RemoveAll(target) // a half-written import must not linger
		return "", err
	}
	return final, nil
}

// freeProfileName returns name, or the first suffixed variant not yet present
// under profilesRoot. Every returned name satisfies the profile-name
// contract: the suffix room is carved out of names that would grow past the
// 32-character bound.
func freeProfileName(profilesRoot, name string) string {
	suffix := 0
	for {
		candidate := name
		if suffix > 0 {
			s := fmt.Sprintf("-%d", suffix+1)
			candidate = name + s
			if len(candidate) > 32 {
				candidate = name[:32-len(s)] + s
			}
		}
		if _, err := os.Stat(filepath.Join(profilesRoot, candidate)); os.IsNotExist(err) {
			return candidate
		}
		suffix++
	}
}

// copyDirContents copies the entire tree rooted at source into target
// (target is created). Regular files, directories, and symlinks are
// reproduced; anything else (sockets, devices) fails the import.
func copyDirContents(source, target string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("browser: walk import source: %w", err)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("browser: relativize import path: %w", err)
		}
		dest := filepath.Join(target, rel)
		switch {
		case d.IsDir():
			if err := os.MkdirAll(dest, 0o700); err != nil {
				return fmt.Errorf("browser: create import dir: %w", err)
			}
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("browser: read symlink %s: %w", path, err)
			}
			if err := os.Symlink(link, dest); err != nil && !os.IsExist(err) {
				return fmt.Errorf("browser: recreate symlink %s: %w", dest, err)
			}
			return nil
		case d.Type().IsRegular():
			if err := copyFile(path, dest); err != nil {
				return err
			}
			return nil
		default:
			return fmt.Errorf("browser: %s is not a regular file, directory, or symlink; it cannot be imported", path)
		}
	})
}

// copyFile copies one regular file, creating its parent directories as
// needed. Copied files are private to the user: profile data carries logins.
func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("browser: open import source file: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("browser: create import parent dir: %w", err)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("browser: create import target file: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("browser: copy import file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("browser: finish import file: %w", err)
	}
	return nil
}
