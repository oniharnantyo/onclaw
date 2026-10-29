package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// writeArtifact writes one file under the artifacts directory, creating
// parent directories (tests' helper — real turns get the directory from
// ArtifactsDir's on-demand creation).
func writeArtifact(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestSnapshotArtifacts_WalkRules pins the walk rules (design D5): nested
// directories list recursively, directories themselves are not entries,
// dot-prefixed files and dot-prefixed directories are skipped, and symlinks
// — to a file or a directory, inside or outside the tree — are never
// followed.
func TestSnapshotArtifacts_WalkRules(t *testing.T) {
	dir := t.TempDir()
	writeArtifact(t, dir, "report.md", "hello")
	writeArtifact(t, dir, "nested/chapter.md", "chapter")
	writeArtifact(t, dir, "nested/deeper/appendix.md", "appendix")
	writeArtifact(t, dir, ".hidden", "hidden")
	writeArtifact(t, dir, "nested/.secret", "secret")
	writeArtifact(t, dir, ".hiddendir/inner.md", "inner")

	// A symlink to a file outside the tree, and one to a directory outside
	// the tree whose contents would otherwise appear.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "outer.md"), []byte("outer"), 0o600))
	writeArtifact(t, outside, "outerdir/leak.md", "leak")
	require.NoError(t, os.Symlink(filepath.Join(outside, "outer.md"), filepath.Join(dir, "link.md")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "outerdir"), filepath.Join(dir, "linkdir")))

	record, err := snapshotArtifacts(dir)
	require.NoError(t, err)
	got := make([]string, 0, len(record))
	for path := range record {
		got = append(got, path)
	}
	sort.Strings(got)
	require.Equal(t, []string{
		"artifacts/nested/chapter.md",
		"artifacts/nested/deeper/appendix.md",
		"artifacts/report.md",
	}, got)
}

// TestSnapshotDiff_CreatedAndModified covers the diff against real files
// (design D3): a file added during the turn reports created, a file
// rewritten with a different size reports modified, an untouched file is
// omitted, deletions are never reported, and entries sort by path.
func TestSnapshotDiff_CreatedAndModified(t *testing.T) {
	dir := t.TempDir()
	writeArtifact(t, dir, "kept.md", "kept")
	writeArtifact(t, dir, "gone.md", "gone")
	writeArtifact(t, dir, "untouched.md", "left alone")
	before, err := snapshotArtifacts(dir)
	require.NoError(t, err)

	// The turn adds a report, rewrites kept.md (different size), deletes
	// gone.md, and never touches untouched.md.
	writeArtifact(t, dir, "report.md", "report")
	writeArtifact(t, dir, "kept.md", "kept, rewritten")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	after, err := snapshotArtifacts(dir)
	require.NoError(t, err)

	arts, truncated := diffArtifacts(before, after)
	require.False(t, truncated)
	// untouched.md is absent: the exact slice is the omission assertion.
	require.Equal(t, []sessions.Artifact{
		{Path: "artifacts/kept.md", Size: int64(len("kept, rewritten")), Change: sessions.ArtifactModified},
		{Path: "artifacts/report.md", Size: int64(len("report")), Change: sessions.ArtifactCreated},
	}, arts)
}

// TestSnapshotDiff_MtimeOnlyChange pins the mtime half of the modified rule
// (design D3): a same-size file whose only change is its modification time
// (pinned via os.Chtimes, as an archive extraction would look) still
// reports modified.
func TestSnapshotDiff_MtimeOnlyChange(t *testing.T) {
	dir := t.TempDir()
	writeArtifact(t, dir, "report.md", "abc")
	before, err := snapshotArtifacts(dir)
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "report.md"), future, future))
	after, err := snapshotArtifacts(dir)
	require.NoError(t, err)

	arts, truncated := diffArtifacts(before, after)
	require.False(t, truncated)
	require.Equal(t, []sessions.Artifact{
		{Path: "artifacts/report.md", Size: 3, Change: sessions.ArtifactModified},
	}, arts)
}

// TestDiffArtifacts_CapAndSort pins the bound (design D4): more qualifying
// entries than sessions.MaxArtifacts yields exactly the cap's worth of
// path-sorted entries with the truncation flag set; exactly at the cap the
// flag stays unset.
func TestDiffArtifacts_CapAndSort(t *testing.T) {
	after := make(artifactRecord)
	for i := 0; i < sessions.MaxArtifacts+5; i++ {
		after[fmt.Sprintf("artifacts/f-%03d.md", i)] = artifactFile{size: int64(i), mtime: time.Unix(int64(i), 0)}
	}
	arts, truncated := diffArtifacts(artifactRecord{}, after)
	require.True(t, truncated)
	require.Len(t, arts, sessions.MaxArtifacts)
	for i, art := range arts {
		require.Equal(t, fmt.Sprintf("artifacts/f-%03d.md", i), art.Path)
		require.Equal(t, sessions.ArtifactCreated, art.Change)
	}

	// Exactly at the cap: everything reports, nothing truncates.
	after = make(artifactRecord)
	for i := 0; i < sessions.MaxArtifacts; i++ {
		after[fmt.Sprintf("artifacts/f-%03d.md", i)] = artifactFile{}
	}
	arts, truncated = diffArtifacts(artifactRecord{}, after)
	require.False(t, truncated)
	require.Len(t, arts, sessions.MaxArtifacts)
}

// TestListArtifactsSince_Predicate pins the cold-resume listing (design
// D3): files whose mtime is at or after the predicate are reported with the
// change kind omitted, older files are left out.
func TestListArtifactsSince_Predicate(t *testing.T) {
	dir := t.TempDir()
	writeArtifact(t, dir, "old.md", "old")
	writeArtifact(t, dir, "new.md", "new")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "old.md"), past, past))
	since := time.Now().Add(-time.Minute)

	arts, truncated, err := listArtifactsSince(dir, since)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, []sessions.Artifact{{Path: "artifacts/new.md", Size: 3}}, arts,
		"the older file is omitted; the newer one carries no change kind")

	// A predicate before everything lists both.
	arts, truncated, err = listArtifactsSince(dir, time.Now().Add(-2*time.Hour))
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, arts, 2)
	for _, art := range arts {
		require.Empty(t, art.Change, "the cold-resume listing never claims a change kind")
	}
}

// TestArtifactsDir_BlockedByRegularFile covers the unusable-path case (spec
// chat-artifacts "Artifacts path blocked by a regular file"): a regular
// file named artifacts makes ArtifactsDir fail, which the runner turns into
// a logged, detection-disabled turn — not a failed one.
func TestArtifactsDir_BlockedByRegularFile(t *testing.T) {
	root := t.TempDir()
	const slug = "tester"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, slug, workspaces.DirArtifacts), []byte("not a directory"), 0o600))

	_, err := workspaces.NewManager(root).ArtifactsDir(slug)
	require.Error(t, err)
}
