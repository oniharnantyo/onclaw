package promptdocs

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// SweepStrayPromptFiles deletes stray prompt files from agent workspace
// directories: base-prompt documents seeded into existing workspaces by
// earlier versions (markdown-card-elements D8) and the BOOTSTRAP.md/.bak
// leftovers of the removed birth-sequence feature (remove-bootstrap-doc).
// The L1 base prompt is injected into every instruction per build, so a
// seeded AGENTS.md would surface stale content to the agent at
// /workspace/AGENTS.md through the jailed mount — the injected prompt
// supersedes it. The birth ritual is gone entirely, so its leftover would
// otherwise keep the ritual text readable to the model at
// /workspace/BOOTSTRAP.md.
//
// Only these file names are ever removed: generated documents
// (IDENTITY/SOUL), their backups, HEARTBEAT, skills, and anything else in
// the directories are untouched. A directory that does not exist, or that
// holds none of the targets, is not an error — there is nothing to sweep.
// Every removal is logged.
func SweepStrayPromptFiles(dirs []string) (removed []string, err error) {
	targets := append([]string{basePromptFileName}, bootstrapFileNames...)
	for _, dir := range dirs {
		for _, name := range targets {
			target := filepath.Join(dir, name)
			if _, statErr := os.Stat(target); errors.Is(statErr, fs.ErrNotExist) {
				continue
			} else if statErr != nil {
				return removed, fmt.Errorf("stat %s: %w", target, statErr)
			}
			if rmErr := os.Remove(target); rmErr != nil {
				return removed, fmt.Errorf("remove %s: %w", target, rmErr)
			}
			slog.Info("removed stray prompt file from agent workspace", "path", target)
			removed = append(removed, target)
		}
	}
	return removed, nil
}
