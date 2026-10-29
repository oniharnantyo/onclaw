package memory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ArchiveNote moves one fact note below the archive floor, or superseded by
// another note, into memory/archive/ (design D11): a move, never a delete —
// the file arrives with its content intact, and the archive directory is
// created on demand. If an archived file of the same name already exists,
// the live note is left in place rather than clobbering it (nothing is ever
// lost); the caller sees an error carrying the reason.
func ArchiveNote(l Layout, name string) error {
	src := l.NotePath(name)
	dst := filepath.Join(l.ArchiveDir(), name+".md")

	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("memory: archive already holds %s; leaving %s in place", dst, src)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("memory: stat %s: %w", dst, err)
	}
	if err := os.MkdirAll(l.ArchiveDir(), dirPerm); err != nil {
		return fmt.Errorf("memory: create %s: %w", l.ArchiveDir(), err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("memory: archive %s: %w", name, err)
	}
	return nil
}

// RestoreNote moves an archived note back into memory/ — the human- or
// agent-side half of the archive contract (spec agent-memory: moving it back
// restores it). The next sweep re-ingests the file (its modification time is
// fresh) and lists it in the index again.
func RestoreNote(l Layout, name string) error {
	src := filepath.Join(l.ArchiveDir(), name+".md")
	dst := l.NotePath(name)

	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("memory: %s already holds a live %s", l.Dir, name)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("memory: stat %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("memory: restore %s: %w", name, err)
	}
	return nil
}

// ListArchivedNotes returns the archived note names, sorted. Files whose
// names are not .md notes are ignored.
func ListArchivedNotes(l Layout) ([]string, error) {
	entries, err := os.ReadDir(l.ArchiveDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read %s: %w", l.ArchiveDir(), err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}

// archiveSkips notes a note the sweep wanted to archive but could not, with
// its reason — the sweep logs it and moves on (never fatal).
func archiveSkips(name, reason string) {
	slog.Warn("memory: skipping archival", "note", name, "reason", reason)
}
