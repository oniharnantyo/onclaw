package promptdocs

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// SweepSeededBasePrompts deletes stray seeded base-prompt documents from
// agent workspace directories (markdown-card-elements D8). The L1 base prompt
// is injected into every instruction per build, so a seeded AGENTS.md left in
// an existing workspace would surface stale content to the agent at
// /workspace/AGENTS.md through the jailed mount — the injected prompt
// supersedes it.
//
// Only the base-prompt file name is ever removed: generated documents
// (IDENTITY/SOUL/BOOTSTRAP), their backups, HEARTBEAT, skills, and anything
// else in the directories are untouched. A directory that does not exist, or
// that holds no base prompt, is not an error — there is nothing to sweep.
// Every removal is logged.
func SweepSeededBasePrompts(dirs []string) (removed []string, err error) {
	for _, dir := range dirs {
		target := filepath.Join(dir, basePromptFileName)
		if _, statErr := os.Stat(target); errors.Is(statErr, fs.ErrNotExist) {
			continue
		} else if statErr != nil {
			return removed, fmt.Errorf("stat %s: %w", target, statErr)
		}
		if rmErr := os.Remove(target); rmErr != nil {
			return removed, fmt.Errorf("remove %s: %w", target, rmErr)
		}
		slog.Info("removed seeded base prompt from agent workspace", "path", target)
		removed = append(removed, target)
	}
	return removed, nil
}
