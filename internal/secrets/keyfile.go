package secrets

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveKeyfilePath resolves the master.key path relative to the database file path.
func ResolveKeyfilePath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "master.key")
}

// GetOrCreateKeyfileKEK reads the key from keyfilePath if it exists, ensuring permissions are 0600.
// If it does not exist, it generates 32 random bytes and writes them to keyfilePath with 0600 permissions.
func GetOrCreateKeyfileKEK(keyfilePath string) ([]byte, error) {
	info, err := os.Stat(keyfilePath)
	if err == nil {
		// Keyfile exists. Assert its permissions are 0600. If wider, refuse to operate.
		mode := info.Mode().Perm()
		if (mode & 0177) != 0 {
			return nil, fmt.Errorf("keyfile permissions %04o are too wide, must be 0600", mode)
		}

		kek, err := os.ReadFile(keyfilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read keyfile: %w", err)
		}
		if len(kek) != 32 {
			return nil, fmt.Errorf("keyfile must be exactly 32 bytes, got %d", len(kek))
		}
		return kek, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check keyfile: %w", err)
	}

	// Ensure parent directory exists before writing
	if err := os.MkdirAll(filepath.Dir(keyfilePath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory for keyfile: %w", err)
	}

	// Keyfile does not exist, generate 32 random bytes.
	kek, err := GenerateDEK()
	if err != nil {
		return nil, fmt.Errorf("failed to generate KEK: %w", err)
	}

	// Write to keyfile path with 0600 permissions.
	err = os.WriteFile(keyfilePath, kek, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to write keyfile: %w", err)
	}

	return kek, nil
}
