package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolve returns the data directory layout under the user's home directory.
// The home directory is resolved through the operating environment (the HOME
// environment variable on Unix), so tests can isolate the layout by setting
// HOME to a temporary directory.
func Resolve() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, fmt.Errorf("resolve home directory: %w", err)
	}
	root := filepath.Join(home, ".buddies")
	return Layout{
		Root:        root,
		DBPath:      filepath.Join(root, "buddies.db"),
		Skills:      filepath.Join(root, "skills"),
		Workspaces:  filepath.Join(root, "workspaces"),
		Attachments: filepath.Join(root, "attachments"),
		Browser:     filepath.Join(root, "browser"),
		Logs:        filepath.Join(root, "logs"),
	}, nil
}
