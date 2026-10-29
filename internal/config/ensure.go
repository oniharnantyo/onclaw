package config

import (
	"fmt"
	"os"
)

// dirPerm is the permission for the data directory and its subdirectories.
// The database holds provider credentials in the clear for the base design,
// so the layout stays private to the user (design risk note).
const (
	dirPerm  os.FileMode = 0700
	filePerm os.FileMode = 0600
)

// Ensure creates the layout idempotently: the root directory, the skills,
// workspaces, attachments, knowledge, browser, and logs subdirectories
// (mode 0700), and an empty database file (mode 0600). Calling it on an
// existing layout is a no-op — existing content is never modified or removed.
func (l Layout) Ensure() error {
	for _, dir := range []string{l.Root, l.Skills, l.Workspaces, l.Attachments, l.Knowledge, l.Browser, l.Logs} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(l.DBPath, os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("create database file %s: %w", l.DBPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close database file %s: %w", l.DBPath, err)
	}
	return nil
}

// EnsureMemory creates one agent's workspace memory directory and its
// archive subdirectory on demand (spec agent-memory: created the first time
// a turn runs for the agent, so existing workspaces need no migration). It
// is idempotent — existing content is never modified or removed.
func (l Layout) EnsureMemory(slug string) error {
	for _, dir := range []string{l.MemoryDir(slug), l.MemoryArchiveDir(slug)} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}
