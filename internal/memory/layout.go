package memory

import (
	"fmt"
	"os"
)

// dirPerm and filePerm mirror the app layout's private-to-user modes
// (internal/config): memory files may hold personal facts.
const (
	dirPerm  os.FileMode = 0700
	filePerm os.FileMode = 0600
)

// EnsureLayout creates the memory directory and its archive subdirectory on
// demand (spec agent-memory: the directory is created the first time a turn
// runs for the agent, so existing workspaces need no migration). It is
// idempotent: calling it on an existing layout is a no-op and never touches
// content already inside the tree.
func EnsureLayout(l Layout) error {
	for _, dir := range []string{l.Dir, l.ArchiveDir()} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("memory: create %s: %w", dir, err)
		}
	}
	return nil
}
