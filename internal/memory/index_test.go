package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The lexical index tests (add-agent-memory tasks.md 5.1, design D5): a
// two-term match outranks a one-term match, no hit is an empty result,
// malformed notes are skipped, and the index rebuilds only after a file's
// mtime changed.

// newTestLayout points a layout at a fresh workspace and returns it.
func newTestLayout(t *testing.T) Layout {
	t.Helper()
	return NewLayout(t.TempDir())
}

// writeIndexNote writes one fact note with the given body terms into the memory
// directory and returns its path.
func writeIndexNote(t *testing.T, layout Layout, name, description, body string) string {
	t.Helper()
	require.NoError(t, EnsureLayout(layout))
	path := layout.NotePath(name)
	raw := "---\nname: " + name + "\ndescription: " + description + "\ntype: reference\n---\n\n" + body
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	return path
}

// searchNames runs one query and returns the hit names in rank order.
func searchNames(t *testing.T, ix *Index, query string) []string {
	t.Helper()
	hits, err := ix.Search(context.Background(), query, 0)
	require.NoError(t, err)
	names := make([]string, 0, len(hits))
	for _, h := range hits {
		names = append(names, h.Name)
	}
	return names
}

// TestIndex_TwoTermMatchRanksFirst pins the overlap ranking (spec agent-memory
// "Lexical hit ranks by match strength"): of two notes matching the query, the
// one matching more terms ranks first, and each carries an excerpt.
func TestIndex_TwoTermMatchRanksFirst(t *testing.T) {
	layout := newTestLayout(t)
	_ = writeIndexNote(t, layout, "stack-choice",
		"The service runs on kubernetes and postgres",
		"We chose kubernetes for orchestration and postgres for storage.")
	_ = writeIndexNote(t, layout, "db-notes",
		"Notes about postgres",
		"postgres backups run nightly; nothing about the other topic here.")

	ix := NewIndex(layout)
	hits, err := ix.Search(context.Background(), "kubernetes postgres", 0)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "stack-choice", hits[0].Name, "the note matching both terms ranks first")
	require.Contains(t, namesOf(hits), "db-notes", "the one-term note is returned too, ranked lower")
	require.Greater(t, hits[0].Score, hits[1].Score)
	require.NotEmpty(t, hits[0].Excerpt, "every hit carries its matched excerpt")
	require.Equal(t, "memory/stack-choice.md", hits[0].Path, "paths are workspace-relative")
}

// TestIndex_NameBoost pins the frontmatter boost (design D5): a term matched in
// a note's name outranks the same term matched only in another note's body.
func TestIndex_NameBoost(t *testing.T) {
	layout := newTestLayout(t)
	_ = writeIndexNote(t, layout, "terraform",
		"Infrastructure as code",
		"terraform state lives in the cloud backend.")
	_ = writeIndexNote(t, layout, "deploy-runbook",
		"How we ship",
		"deploys mention terraform once and then move on to the pipeline.")

	ix := NewIndex(layout)
	hits, err := ix.Search(context.Background(), "terraform", 0)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "terraform", hits[0].Name, "the name match outranks the body match")
}

// TestIndex_NoHitIsEmpty pins the miss behavior (spec agent-memory "No match is
// an empty result"): a query matching nothing returns an empty result and no
// error.
func TestIndex_NoHitIsEmpty(t *testing.T) {
	layout := newTestLayout(t)
	_ = writeIndexNote(t, layout, "unrelated", "Something else", "entirely different words.")

	ix := NewIndex(layout)
	hits, err := ix.Search(context.Background(), "kubernetes", 0)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// TestIndex_MissingDirectoryIsEmpty pins the fail-soft rule: no memory
// directory is an empty result, not an error.
func TestIndex_MissingDirectoryIsEmpty(t *testing.T) {
	ix := NewIndex(NewLayout(filepath.Join(t.TempDir(), "workspace")))
	hits, err := ix.Search(context.Background(), "anything", 0)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// TestIndex_SkipsMalformedNotes pins the malformed-note rule (spec
// agent-memory: a malformed note is skipped, the rest still searches).
func TestIndex_SkipsMalformedNotes(t *testing.T) {
	layout := newTestLayout(t)
	require.NoError(t, EnsureLayout(layout))
	require.NoError(t, os.WriteFile(layout.NotePath("broken"),
		[]byte("---\nno name field here\n"), 0o600))
	_ = writeIndexNote(t, layout, "valid", "A valid note", "the kubernetes note survives.")

	ix := NewIndex(layout)
	require.Equal(t, []string{"valid"}, searchNames(t, ix, "kubernetes"))
	require.Empty(t, searchNames(t, ix, "field"), "the malformed note is not searchable either")
}

// TestIndex_IndexesDailyAndIndexFiles pins the corpus (spec agent-memory: the
// search covers fact-note frontmatter and bodies, daily notes, and the index).
func TestIndex_IndexesDailyAndIndexFiles(t *testing.T) {
	layout := newTestLayout(t)
	require.NoError(t, EnsureLayout(layout))
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(layout.DailyPath(day),
		[]byte("# Work log\n\nObserved the reindex flakiness in the ingest worker.\n"), 0o600))
	require.NoError(t, os.WriteFile(layout.IndexPath(),
		[]byte("# Memory index\n\n- reindex: the ingest worker note\n"), 0o600))

	ix := NewIndex(layout)
	names := searchNames(t, ix, "reindex flakiness")
	require.Equal(t, "2026-10-27", names[0], "the daily note matching both terms ranks first")
	require.Contains(t, names, "MEMORY", "the index file matches on its shared term")
}

// TestIndex_Tokenizer pins the tokenizer contract (design D5): identifiers —
// snake_case and camelCase — and backticked spans stay whole; plain prose
// splits; everything lowercases.
func TestIndex_Tokenizer(t *testing.T) {
	require.Equal(t, []string{"memory_search"}, Tokenize("memory_search"))
	require.Equal(t, []string{"memorysearch"}, Tokenize("MemorySearch"))
	require.Equal(t, []string{"some_tool_name", "stays", "whole"}, Tokenize("`some_tool_name` stays whole"))
	require.Equal(t, []string{"dark", "mode", "preference"}, Tokenize("Dark-Mode preference!"))
	require.Equal(t, []string{"run", "the", "tests"}, Tokenize("Run the tests"))
	require.Equal(t, []string{"oci", "registry"}, Tokenize("OCI registry"))
}

// TestIndex_RebuildOnlyAfterMtimeChange pins the refresh policy (design D5):
// unchanged files reuse the built index; an mtime change rebuilds it and the
// new content becomes searchable.
func TestIndex_RebuildOnlyAfterMtimeChange(t *testing.T) {
	layout := newTestLayout(t)
	path := writeIndexNote(t, layout, "greeting", "A greeting note", "the user greets everyone warmly.")

	ix := NewIndex(layout)
	_, err := ix.Search(context.Background(), "greeting", 0)
	require.NoError(t, err)
	require.Equal(t, 1, ix.rebuilds(), "the first search builds once")

	// An unchanged tree reuses the built index.
	_, err = ix.Search(context.Background(), "greeting", 0)
	require.NoError(t, err)
	require.Equal(t, 1, ix.rebuilds(), "no rebuild without an mtime change")

	// Rewriting the file with an explicit, moved mtime rebuilds it — and the
	// new content is searchable.
	require.NoError(t, os.WriteFile(path, []byte("---\nname: greeting\ndescription: A greeting note\ntype: reference\n---\n\nnow mentions zopfli compression.\n"), 0o600))
	future := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, future, future))
	require.Equal(t, []string{"greeting"}, searchNames(t, ix, "zopfli"))
	require.Equal(t, 2, ix.rebuilds(), "the mtime change rebuilt the index")

	// A file added with a fresh mtime rebuilds; a file removed rebuilds too.
	added := writeIndexNote(t, layout, "second", "Another note", "unrelated words.")
	require.NoError(t, os.Chtimes(added, future, future))
	_, err = ix.Search(context.Background(), "greeting", 0)
	require.NoError(t, err)
	require.Equal(t, 3, ix.rebuilds(), "a new file rebuilds the index")
	require.NoError(t, os.Remove(added))
	_, err = ix.Search(context.Background(), "greeting", 0)
	require.NoError(t, err)
	require.Equal(t, 4, ix.rebuilds(), "a removed file rebuilds the index")
}

// namesOf maps hits to their names.
func namesOf(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}
