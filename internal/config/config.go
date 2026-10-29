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
		Knowledge:   filepath.Join(root, "knowledge"),
		Browser:     filepath.Join(root, "browser"),
		Logs:        filepath.Join(root, "logs"),
	}, nil
}

// MemoryDir returns one agent's workspace memory directory
// (workspaces/<slug>/memory): the curated index MEMORY.md, the fact notes,
// and the daily notes live here (add-agent-memory design D1).
func (l Layout) MemoryDir(slug string) string {
	return filepath.Join(l.Workspaces, slug, MemoryDirName)
}

// MemoryArchiveDir returns the archive subdirectory of one agent's workspace
// memory directory (workspaces/<slug>/memory/archive) — where below-floor
// and superseded notes are moved, never deleted.
func (l Layout) MemoryArchiveDir(slug string) string {
	return filepath.Join(l.MemoryDir(slug), MemoryArchiveDirName)
}
