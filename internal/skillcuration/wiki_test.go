package skillcuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func testPage(slug, title, body string, runs ...string) Page {
	return Page{
		Slug:         slug,
		Title:        title,
		Status:       PageActive,
		EvidenceRuns: runs,
		Body:         body,
	}
}

func mustCreate(t *testing.T, w *Wiki, page Page) {
	t.Helper()
	if err := w.Create(page); err != nil {
		t.Fatalf("create page %s: %v", page.Slug, err)
	}
}

// TestWikiCreateRoundTrips: a created page reads back with its title,
// citations, and body intact, and lists as active.
func TestWikiCreateRoundTrips(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("deploy-rollback", "Deploy rollback", "Check the health endpoint before promoting.", "sess-a", "sess-b"))

	page, err := w.Page("deploy-rollback")
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if page.Title != "Deploy rollback" || page.Status != PageActive || page.SupersededBy != "" {
		t.Fatalf("unexpected page header: %+v", page)
	}
	if len(page.EvidenceRuns) != 2 || page.EvidenceRuns[0] != "sess-a" || page.EvidenceRuns[1] != "sess-b" {
		t.Fatalf("evidence runs must round-trip in order, got %v", page.EvidenceRuns)
	}
	if page.Body != "Check the health endpoint before promoting." {
		t.Fatalf("body must round-trip, got %q", page.Body)
	}

	list, err := w.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Slug != "deploy-rollback" || list[0].Status != PageActive {
		t.Fatalf("expected the created page listed active, got %+v", list)
	}
}

// TestWikiCreateDuplicateRejected: a second create with the same slug is an
// error — overwriting is Update's job.
func TestWikiCreateDuplicateRejected(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("dup", "First", "body one", "sess-a"))
	if err := w.Create(testPage("dup", "Second", "body two", "sess-b")); err == nil {
		t.Fatal("expected duplicate create to error")
	}
	page, err := w.Page("dup")
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if page.Title != "First" || page.Body != "body one" {
		t.Fatalf("duplicate create must not clobber, got %+v", page)
	}
}

// TestWikiSupersedeKeepsOldReadableWithPointer: the successor page is
// written, the old page remains readable with a Superseded-By pointer, and
// nothing is removed (spec: "Stale pattern is superseded, not erased").
func TestWikiSupersedeKeepsOldReadableWithPointer(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("retry-v1", "Retry v1", "Retry once immediately.", "sess-old"))

	if err := w.Supersede("retry-v1", testPage("retry-v2", "Retry v2", "Back off exponentially.", "sess-new")); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	old, err := w.Page("retry-v1")
	if err != nil {
		t.Fatalf("superseded page must remain readable: %v", err)
	}
	if old.Status != PageSuperseded || old.SupersededBy != "retry-v2" {
		t.Fatalf("superseded page must carry the successor pointer, got %+v", old)
	}
	if old.Body != "Retry once immediately." {
		t.Fatalf("superseded page must keep its body, got %q", old.Body)
	}

	successor, err := w.Page("retry-v2")
	if err != nil {
		t.Fatalf("read successor: %v", err)
	}
	if successor.Status != PageActive || successor.SupersededBy != "" {
		t.Fatalf("successor must be active, got %+v", successor)
	}

	// Nothing removed: both pages list, with statuses.
	list, err := w.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected both pages listed, got %+v", list)
	}
	if list[0].Slug != "retry-v1" || list[0].Status != PageSuperseded || list[1].Slug != "retry-v2" || list[1].Status != PageActive {
		t.Fatalf("unexpected listing: %+v", list)
	}
}

// TestWikiSupersedeRejects: unknown old page, existing successor, self-
// supersede, and re-supersede of an already-superseded page all fail.
func TestWikiSupersedeRejects(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("live", "Live", "body", "sess-a"))
	mustCreate(t, w, testPage("taken", "Taken", "body", "sess-a"))

	if err := w.Supersede("missing", testPage("s1", "S", "b", "sess-a")); !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("superseding an unknown page must be ErrPageNotFound, got %v", err)
	}
	if err := w.Supersede("live", testPage("taken", "S", "b", "sess-a")); err == nil {
		t.Fatal("superseding onto an existing slug must error")
	}
	if err := w.Supersede("live", testPage("live", "S", "b", "sess-a")); err == nil {
		t.Fatal("self-supersede must error")
	}
	if err := w.Supersede("live", testPage("next", "S", "b", "sess-a")); err != nil {
		t.Fatalf("seed supersede: %v", err)
	}
	if err := w.Supersede("live", testPage("next2", "S", "b", "sess-a")); err == nil {
		t.Fatal("re-superseding an already-superseded page must error")
	}
}

// TestWikiUpdateRewritesActiveOnly: an active page's content is replaced; a
// superseded page is immutable.
func TestWikiUpdateRewritesActiveOnly(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("evolve", "Evolve", "v1", "sess-a"))

	if err := w.Update(testPage("evolve", "Evolve", "v2", "sess-b")); err != nil {
		t.Fatalf("update: %v", err)
	}
	page, err := w.Page("evolve")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if page.Body != "v2" || len(page.EvidenceRuns) != 1 || page.EvidenceRuns[0] != "sess-b" {
		t.Fatalf("update must replace content and citations, got %+v", page)
	}

	if err := w.Supersede("evolve", testPage("evolve-2", "E2", "b", "sess-a")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if err := w.Update(testPage("evolve", "Evolve", "v3", "sess-a")); err == nil {
		t.Fatal("updating a superseded page must error")
	}
	if err := w.Update(testPage("missing", "M", "b", "sess-a")); !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("updating an unknown page must be ErrPageNotFound, got %v", err)
	}
}

// TestWikiMergeIntoFreshPage: merging two pages into a new canonical page
// creates it and retires both sources with pointers (nothing deleted).
func TestWikiMergeIntoFreshPage(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("dup-a", "Dup A", "half the procedure", "sess-a"))
	mustCreate(t, w, testPage("dup-b", "Dup B", "the other half", "sess-b"))

	if err := w.Merge([]string{"dup-a", "dup-b"}, testPage("dup-canonical", "Dup canonical", "the whole procedure", "sess-c")); err != nil {
		t.Fatalf("merge: %v", err)
	}

	canonical, err := w.Page("dup-canonical")
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	if canonical.Status != PageActive || canonical.Body != "the whole procedure" {
		t.Fatalf("canonical must be active with merged body, got %+v", canonical)
	}
	for _, src := range []string{"dup-a", "dup-b"} {
		page, err := w.Page(src)
		if err != nil {
			t.Fatalf("merged source %s must remain readable: %v", src, err)
		}
		if page.Status != PageSuperseded || page.SupersededBy != "dup-canonical" {
			t.Fatalf("merged source %s must point at the canonical, got %+v", src, page)
		}
	}
}

// TestWikiMergeIntoExistingSource: merging into one of the sources updates
// that page in place and retires the sibling.
func TestWikiMergeIntoExistingSource(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("keep", "Keep", "kept body", "sess-a"))
	mustCreate(t, w, testPage("drop", "Drop", "dropped body", "sess-b"))

	if err := w.Merge([]string{"keep", "drop"}, testPage("keep", "Keep", "merged body", "sess-c")); err != nil {
		t.Fatalf("merge: %v", err)
	}

	kept, err := w.Page("keep")
	if err != nil {
		t.Fatalf("read kept: %v", err)
	}
	if kept.Status != PageActive || kept.Body != "merged body" {
		t.Fatalf("canonical source must be updated in place, got %+v", kept)
	}
	dropped, err := w.Page("drop")
	if err != nil {
		t.Fatalf("read dropped: %v", err)
	}
	if dropped.Status != PageSuperseded || dropped.SupersededBy != "keep" {
		t.Fatalf("sibling must point at the canonical, got %+v", dropped)
	}
}

// TestWikiMergeRejects: fewer than two sources, unknown sources, an existing
// fresh target, and superseded sources all fail without side effects.
func TestWikiMergeRejects(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("a", "A", "body", "sess-a"))
	mustCreate(t, w, testPage("b", "B", "body", "sess-a"))
	mustCreate(t, w, testPage("exists", "E", "body", "sess-a"))

	if err := w.Merge([]string{"a"}, testPage("m", "M", "b", "sess-a")); err == nil {
		t.Fatal("merge with one source must error")
	}
	if err := w.Merge([]string{"a", "missing"}, testPage("m", "M", "b", "sess-a")); err == nil {
		t.Fatal("merge with an unknown source must error")
	}
	if err := w.Merge([]string{"a", "b"}, testPage("exists", "E", "b", "sess-a")); err == nil {
		t.Fatal("merge onto an existing non-source slug must error")
	}
	if err := w.Merge([]string{"a", "a"}, testPage("m", "M", "b", "sess-a")); err == nil {
		t.Fatal("merge listing a source twice must error")
	}
	// Nothing was written by the failed merges.
	list, err := w.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("failed merges must not write pages, got %+v", list)
	}
}

// TestWikiLogsAppendOnly: every operation appends its log line and earlier
// lines are never truncated or rewritten.
func TestWikiLogsAppendOnly(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	mustCreate(t, w, testPage("p1", "P1", "body", "sess-a"))
	if err := w.Update(testPage("p1", "P1", "body2", "sess-b")); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := w.Supersede("p1", testPage("p2", "P2", "body3", "sess-c")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if err := w.Merge([]string{"p1", "p1-x"}, testPage("p3", "P3", "body4", "sess-d")); err == nil {
		t.Fatal("expected merge with unknown source to fail (no log line)")
	}
	mustCreate(t, w, testPage("p1-x", "P1X", "body", "sess-d"))
	if err := w.Merge([]string{"p1-x", "p1-y"}, testPage("p3", "P3", "body4", "sess-d")); err == nil {
		t.Fatal("expected merge with superseded source to fail (no log line)")
	}
	mustCreate(t, w, testPage("p1-y", "P1Y", "body", "sess-e"))
	if err := w.Merge([]string{"p1-x", "p1-y"}, testPage("p3", "P3", "body4", "sess-d")); err != nil {
		t.Fatalf("merge: %v", err)
	}

	lines, err := w.ReadLog()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	want := []string{"create p1", "update p1", "supersede p2 (p1)", "create p1-x", "create p1-y", "merge p3 (p1-x,p1-y)"}
	if len(lines) != len(want) {
		t.Fatalf("expected %d log lines, got %d:\n%s", len(want), len(lines), strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if !strings.Contains(line, want[i]) {
			t.Fatalf("log line %d = %q, want it to contain %q", i, line, want[i])
		}
		if !strings.HasPrefix(line, "- ") || !strings.Contains(line, "evidence=") {
			t.Fatalf("log line must carry timestamp and evidence citations: %q", line)
		}
	}

	// The supersede line cites both the successor's and the old page's
	// current evidence (p1's citations were replaced by the update).
	if !strings.Contains(lines[2], "sess-c") || !strings.Contains(lines[2], "sess-b") {
		t.Fatalf("supersede log must cite both pages' evidence: %q", lines[2])
	}
}

// TestWikiAppendLogConcurrentWritesInterleaveWholeLines: concurrent appends
// never tear a line — every line in the log parses as a complete entry.
func TestWikiAppendLogConcurrentWritesInterleaveWholeLines(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slug := "page-" + string(rune('a'+i%26))
			if err := w.AppendLog(LogEntry{Operation: "create", Page: slug, EvidenceRuns: []string{"sess-x"}}); err != nil {
				t.Errorf("append log: %v", err)
			}
		}(i)
	}
	wg.Wait()

	lines, err := w.ReadLog()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(lines) != 32 {
		t.Fatalf("expected 32 whole lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") || !strings.HasSuffix(line, "evidence=sess-x") {
			t.Fatalf("torn log line: %q", line)
		}
	}
}

// TestWikiAtomicCrashBetweenStageAndRename: a crash between staging the temp
// file and the rename leaves a dot-prefixed leftover that no reader treats
// as a page — no partial page ever becomes visible.
func TestWikiAtomicCrashBetweenStageAndRename(t *testing.T) {
	base := filepath.Join(t.TempDir(), "skillwiki")
	w := NewWiki(base)
	mustCreate(t, w, testPage("real", "Real", "body", "sess-a"))

	// Simulate the crashed write: a staged temp file left in the patterns
	// directory, never renamed.
	patterns := filepath.Join(base, "patterns")
	if err := os.WriteFile(filepath.Join(patterns, ".ghost.md.tmp-123"), []byte("# Ghost\n\nStatus: active\nEvidence-Runs: sess-x\n\npartial"), 0o644); err != nil {
		t.Fatalf("seed crashed temp file: %v", err)
	}

	list, err := w.List()
	if err != nil {
		t.Fatalf("list must tolerate a crashed temp file: %v", err)
	}
	if len(list) != 1 || list[0].Slug != "real" {
		t.Fatalf("the staged temp file must never appear as a page, got %+v", list)
	}
	if _, err := w.Page(".ghost"); err == nil {
		t.Fatal("a dot-prefixed name must not read as a page")
	}
	// And the wiki keeps working across the leftover.
	mustCreate(t, w, testPage("next", "Next", "body", "sess-b"))
}

// TestWikiRejectsTraversalAndBadSlugs: model-supplied page names can never
// traverse or escape the patterns directory.
func TestWikiRejectsTraversalAndBadSlugs(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	bad := []string{
		"../evil", "../../etc/passwd", "a/b", "..", ".", "", "-lead", "trail-",
		"UPPER", "under_score", "space space", "dot.name", strings.Repeat("x", 64),
	}
	for _, slug := range bad {
		if err := w.Create(testPage(slug, "T", "b", "sess-a")); err == nil {
			t.Fatalf("create with slug %q must be rejected", slug)
		}
		if _, err := w.Page(slug); err == nil {
			t.Fatalf("read with slug %q must be rejected", slug)
		}
		if err := w.Supersede("ok-page", testPage(slug, "T", "b", "sess-a")); err == nil {
			t.Fatalf("supersede with successor slug %q must be rejected", slug)
		}
	}
	// Nothing was written: the wiki dir does not even exist yet.
	if _, err := os.Stat(filepath.Join(w.Dir(), wikiPatternsDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected pages must not create anything, got %v", err)
	}
}

// TestWikiRejectsPagesWithoutEvidence: every pattern page must cite at least
// one evidence run (spec: "each page citing the evidence runs it was derived
// from") — the file layer refuses to store an uncited page.
func TestWikiRejectsPagesWithoutEvidence(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	if err := w.Create(testPage("uncited", "U", "body")); err == nil {
		t.Fatal("creating an uncited page must error")
	}
	if err := w.Create(testPage("blank-cited", "B", "body", " ")); err == nil {
		t.Fatal("creating a page with a blank citation must error")
	}
}

// TestWikiRejectsMalformedPageFile: a hand-tampered page file fails parsing
// (and therefore listing) instead of silently surfacing garbage.
func TestWikiRejectsMalformedPageFile(t *testing.T) {
	base := filepath.Join(t.TempDir(), "skillwiki")
	w := NewWiki(base)
	for name, content := range map[string]string{
		"no-title":  "just some text\n",
		"no-status": "# T\n\nEvidence-Runs: sess-a\n\nbody\n",
		"no-evide":  "# T\n\nStatus: active\n\nbody\n",
		"bad-head":  "# T\n\nStatus: active\nJunk-Header: x\nEvidence-Runs: sess-a\n\nbody\n",
		"no-body":   "# T\n\nStatus: active\nEvidence-Runs: sess-a\n\n",
		"bad-stat":  "# T\n\nStatus: weird\nEvidence-Runs: sess-a\n\nbody\n",
		"supers-no": "# T\n\nStatus: superseded\nEvidence-Runs: sess-a\n\nbody\n",
		"active-pt": "# T\n\nStatus: active\nSuperseded-By: other\nEvidence-Runs: sess-a\n\nbody\n",
	} {
		patterns := filepath.Join(base, wikiPatternsDirName)
		if err := os.MkdirAll(patterns, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(patterns, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("seed malformed page %s: %v", name, err)
		}
		if _, err := w.Page(name); err == nil {
			t.Fatalf("malformed page %s must fail parsing", name)
		}
		if _, err := w.List(); err == nil {
			t.Fatalf("listing with malformed page %s must fail", name)
		}
	}
}

// TestWikiEmptyWikiReads: a missing wiki directory is the empty wiki, not an
// error.
func TestWikiEmptyWikiReads(t *testing.T) {
	w := NewWiki(filepath.Join(t.TempDir(), "skillwiki"))
	list, err := w.List()
	if err != nil {
		t.Fatalf("list on missing wiki: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty listing, got %+v", list)
	}
	lines, err := w.ReadLog()
	if err != nil {
		t.Fatalf("read log on missing wiki: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected empty log, got %v", lines)
	}
	if _, err := w.Page("absent"); !errors.Is(err, ErrPageNotFound) || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing page must be ErrPageNotFound wrapping domain.ErrNotFound, got %v", err)
	}
}
