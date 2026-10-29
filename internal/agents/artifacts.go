package agents

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// Turn-boundary artifact detection (add-turn-artifacts design D3): the
// runner lists the workspace's artifacts directory when a turn starts,
// lists it again when the turn completes, and the delta rides the
// turn_completed event (design D4). Each listing is one WalkDir over a
// directory the user owns — not a hot path (design D5).

// artifactFile is one listed file's state: its byte size and modification
// time — the pair a diff reads a change from (design D3).
type artifactFile struct {
	size  int64
	mtime time.Time
}

// artifactRecord is a point-in-time listing of the artifacts directory:
// workspace-relative path ("artifacts/report.md", forward slashes) → the
// file's state.
type artifactRecord map[string]artifactFile

// snapshotArtifacts lists dir into a record (design D5): recursive under
// dir, regular files only, dot-prefixed entries — files and directories —
// skipped, symlinks never followed. Keys are "artifacts/" plus the path
// relative to dir with forward slashes, so an entry feeds the frontend's
// workspace-file preview directly.
func snapshotArtifacts(dir string) (artifactRecord, error) {
	record := make(artifactRecord)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("artifacts: relativize %s: %w", path, err)
		}
		if rel == "." {
			return nil
		}
		// Dot-prefixed entries are agent-internal state, not deliverables
		// (design D5): prune a dot directory, skip a dot file.
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Symlinks are never followed (design D5): a link — to a file or a
		// directory, inside or outside the workspace — is not a deliverable.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("artifacts: stat %s: %w", path, err)
		}
		record[filepath.ToSlash(filepath.Join("artifacts", rel))] = artifactFile{size: info.Size(), mtime: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("artifacts: walk %s: %w", dir, err)
	}
	return record, nil
}

// diffArtifacts compares the start record against the completion listing
// (design D3): present at completion and absent at start → created; present
// at both with a different size or mtime → modified; unchanged → omitted.
// Deletions and renames are never reported. Entries sort by path and cap at
// sessions.MaxArtifacts, with truncated reporting a hit cap (design D4).
func diffArtifacts(before, after artifactRecord) (arts []sessions.Artifact, truncated bool) {
	paths := make([]string, 0, len(after))
	for path := range after {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		was, existed := before[path]
		file := after[path]
		change := sessions.ArtifactCreated
		if existed {
			if was.size == file.size && was.mtime.Equal(file.mtime) {
				continue
			}
			change = sessions.ArtifactModified
		}
		if len(arts) == sessions.MaxArtifacts {
			return arts, true
		}
		arts = append(arts, sessions.Artifact{Path: path, Size: file.size, Change: change})
	}
	return arts, false
}

// listArtifactsSince lists the artifacts directory for a cold-resumed turn
// (design D3): every regular file whose modification time is at or after
// since — the turn's persisted user-message time — with the change kind
// omitted, because created-versus-modified cannot be known. Same walk rules,
// ordering, and cap as the diff.
func listArtifactsSince(dir string, since time.Time) (arts []sessions.Artifact, truncated bool, err error) {
	after, err := snapshotArtifacts(dir)
	if err != nil {
		return nil, false, err
	}
	paths := make([]string, 0, len(after))
	for path, file := range after {
		if file.mtime.Before(since) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if len(arts) == sessions.MaxArtifacts {
			return arts, true, nil
		}
		arts = append(arts, sessions.Artifact{Path: path, Size: after[path].size})
	}
	return arts, false, nil
}
