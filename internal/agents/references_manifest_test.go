package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ---------------------------------------------------------------------------
// 5.1/5.2 — the compose-time reference-documents manifest
// ---------------------------------------------------------------------------

// manifestEntryOf extracts the first manifest entry line naming docName.
func manifestEntryOf(manifest, docName string) string {
	for _, line := range strings.Split(manifest, "\n") {
		if strings.HasPrefix(line, "- "+docName+" ") || line == "- "+docName {
			return line
		}
	}
	return ""
}

// TestRenderReferenceManifest_Shapes pins the full-detail entry shape and the
// segment-omission rules: `- <name> — <description> — <pages> — TOC: ...`,
// page count omitted at 0, TOC omitted with no Level<=1 headings.
func TestRenderReferenceManifest_Shapes(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-manifest")

	// Descriptions and TOC headings ride the real indexer: the fixture's md
	// yields Level-1 headings Setup and Webhooks.
	withEverything := h.uploadDoc(t, "full.md", []string{h.atlasID}, nil)
	if _, err := h.svc.UpdateMeta(h.ctx, h.wsID, withEverything.ID, "full.md", "The one-line description"); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	// A no-text-layer PDF: real page count, but no indexable headings — the
	// TOC-less case.
	scanned := h.uploadRaw(t, "scan.pdf", pdfBytesForTest(t, ""), []string{h.atlasID}, nil)
	if scanned.IndexStatus != domain.RefDocIndexNoTextLayer {
		t.Fatalf("fixture scan.pdf status = %q, want no_text_layer", scanned.IndexStatus)
	}

	r := &Runner{references: h.svc}
	docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: h.atlasID})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("visible docs = %d, want 2", len(docs))
	}
	manifest := r.renderReferenceManifest(context.Background(), &agentConfig{
		WorkspaceID:           h.wsID,
		documentSearchEnabled: true,
		visibleDocuments:      docs,
	})

	if !strings.HasPrefix(manifest, "## Reference documents\n") {
		t.Fatalf("manifest must open with its own titled block:\n%s", manifest)
	}
	for _, usage := range []string{"document.search", "document.read", "references/"} {
		if !strings.Contains(manifest, usage) {
			t.Errorf("usage line must teach %q:\n%s", usage, manifest)
		}
	}

	fullEntry := manifestEntryOf(manifest, "full.md")
	wantTOC := "TOC: Setup; Webhooks"
	if !strings.Contains(fullEntry, "The one-line description") || !strings.Contains(fullEntry, wantTOC) {
		t.Errorf("full entry = %q, want the description and %q", fullEntry, wantTOC)
	}
	if strings.Contains(fullEntry, "pages") {
		t.Errorf("markdown entry must omit the page-count segment at 0 pages: %q", fullEntry)
	}

	scanEntry := manifestEntryOf(manifest, "scan.pdf")
	if scanEntry == "" || !strings.Contains(scanEntry, "scan.pdf") {
		t.Fatalf("scanned entry missing: %q", scanEntry)
	}
	if strings.Contains(scanEntry, "TOC:") {
		t.Errorf("TOC-less document must omit the TOC segment: %q", scanEntry)
	}
	if !strings.Contains(scanEntry, "1 pages") {
		t.Errorf("scanned pdf entry must carry its page count: %q", scanEntry)
	}
}

// TestRenderReferenceManifest_TOCProjectsAllChapters is task 1.1's verify
// (fix-reference-document-retrieval): the manifest TOC projects every
// Level<=1 heading in ordinal order — not the old fixed 6-entry mixed-level
// prefix. The fixture's sixth ordinal heading is "4 Operations", so the old
// cap stopped there and chapter 5 — the chapter containing the section a
// user actually asks about (5.1 PostLogin) — never reached the context.
func TestRenderReferenceManifest_TOCProjectsAllChapters(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-toc-chapters")

	const manual = "# 1 Overview\noverview body.\n\n" +
		"## 1.1 Scope\nscope body.\n\n" +
		"# 2 Installation\ninstallation body.\n\n" +
		"## 2.1 Prerequisites\nprerequisites body.\n\n" +
		"# 3 Configuration\nconfiguration body.\n\n" +
		"# 4 Operations\noperations body.\n\n" +
		"# 5 Authentication\nauthentication body.\n\n" +
		"## 5.1 PostLogin\npostlogin payload here.\n"
	h.uploadRaw(t, "postlogin-manual.md", []byte(manual), []string{h.atlasID}, nil)

	docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: h.atlasID})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("visible docs = %d, want 1", len(docs))
	}

	r := &Runner{references: h.svc}
	manifest := r.renderReferenceManifest(h.ctx, &agentConfig{
		WorkspaceID:           h.wsID,
		documentSearchEnabled: true,
		visibleDocuments:      docs,
	})

	entry := manifestEntryOf(manifest, "postlogin-manual.md")
	if entry == "" {
		t.Fatalf("manifest entry missing:\n%s", manifest)
	}

	// Every chapter heading, in ordinal order — subsections excluded; their
	// content is discoverable through the chapter that contains them.
	wantTOC := "TOC: 1 Overview; 2 Installation; 3 Configuration; 4 Operations; 5 Authentication"
	if !strings.Contains(entry, wantTOC) {
		t.Errorf("manifest TOC = %q, want %q (all chapters, ordinal order)", entry, wantTOC)
	}
	// The chapter containing the deep section is present where the fixed
	// 6-entry mixed-level prefix cut it off.
	if !strings.Contains(entry, "5 Authentication") {
		t.Errorf("chapter 5 heading missing from the manifest TOC:\n%s", entry)
	}
	// The level-2 section itself is not the projection.
	if strings.Contains(entry, "5.1 PostLogin") {
		t.Errorf("level-2 heading leaked into the manifest TOC:\n%s", entry)
	}
}

// TestRenderReferenceManifest_SkipPaths pins the skip-entirely contract:
// unwired library, gate-off runs, and empty visible sets render no block.
func TestRenderReferenceManifest_SkipPaths(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-manifest-skip")
	h.uploadDoc(t, "runbook.md", []string{h.atlasID}, nil)
	ctx := context.Background()

	docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: h.atlasID})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}

	// Unwired library: no block even with docs and an open gate.
	unwired := &Runner{}
	if got := unwired.renderReferenceManifest(ctx, &agentConfig{
		WorkspaceID: h.wsID, documentSearchEnabled: true, visibleDocuments: docs,
	}); got != "" {
		t.Errorf("unwired manifest = %q, want empty", got)
	}

	// Gate off: document.search excluded from the post-gate effective set.
	wired := &Runner{references: h.svc}
	if got := wired.renderReferenceManifest(ctx, &agentConfig{
		WorkspaceID: h.wsID, documentSearchEnabled: false, visibleDocuments: docs,
	}); got != "" {
		t.Errorf("gate-off manifest = %q, want empty", got)
	}

	// Nothing visible.
	if got := wired.renderReferenceManifest(ctx, &agentConfig{
		WorkspaceID: h.wsID, documentSearchEnabled: true, visibleDocuments: nil,
	}); got != "" {
		t.Errorf("empty-visible manifest = %q, want empty", got)
	}
}

// TestRenderReferenceManifest_BudgetCollapsesNeverDrops pins 5.2: past the
// budget later entries collapse their TOCs first, then descriptions truncate
// — and every visible document keeps an entry (discovery survives, D6).
func TestRenderReferenceManifest_BudgetCollapsesNeverDrops(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-budget")

	const docCount = 6
	names := make([]string, 0, docCount)
	for i := 0; i < docCount; i++ {
		name := "doc-" + string(rune('a'+i)) + ".md"
		names = append(names, name)
		doc := h.uploadDoc(t, name, []string{h.atlasID}, nil)
		if _, err := h.svc.UpdateMeta(h.ctx, h.wsID, doc.ID, name, "description of "+name+" with some length to truncate"); err != nil {
			t.Fatalf("update meta: %v", err)
		}
	}

	docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: h.atlasID})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(docs) != docCount {
		t.Fatalf("visible docs = %d, want %d", len(docs), docCount)
	}

	r := &Runner{references: h.svc, referenceManifestBudget: 900}
	manifest := r.renderReferenceManifest(context.Background(), &agentConfig{
		WorkspaceID:           h.wsID,
		documentSearchEnabled: true,
		visibleDocuments:      docs,
	})

	// Every entry survives — collapse, never drop (D6).
	got := manifestDocNames(manifest)
	if len(got) != docCount {
		t.Fatalf("manifest entries = %d (%v), want all %d documents", len(got), got, docCount)
	}
	gotSet := make(map[string]struct{}, docCount)
	for _, name := range got {
		gotSet[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := gotSet[name]; !ok {
			t.Errorf("visible document %q disappeared from the manifest", name)
		}
	}

	// Overflow collapsed later entries: at least one entry lost its TOC
	// while at least one early entry kept it (the ordered degradation).
	collapsed, detailed := 0, 0
	for _, line := range strings.Split(manifest, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if strings.Contains(line, "TOC:") {
			detailed++
		} else {
			collapsed++
		}
	}
	if collapsed == 0 || detailed == 0 {
		t.Fatalf("expected mixed detail (collapse the overflow, keep the early entries), got detailed=%d collapsed=%d\n%s", detailed, collapsed, manifest)
	}

	// A tight budget truncates descriptions but keeps every name.
	tight := &Runner{references: h.svc, referenceManifestBudget: 400}
	tightManifest := tight.renderReferenceManifest(context.Background(), &agentConfig{
		WorkspaceID:           h.wsID,
		documentSearchEnabled: true,
		visibleDocuments:      docs,
	})
	tightNames := manifestDocNames(tightManifest)
	if len(tightNames) != docCount {
		t.Fatalf("tight-budget entries = %d, want all %d (entries are never dropped)", len(tightNames), docCount)
	}
	for _, name := range names {
		if !strings.Contains(tightManifest, "- "+name) {
			t.Errorf("tight budget lost the entry for %q:\n%s", name, tightManifest)
		}
	}
}

// TestWithReferenceManifestBudget pins the functional option: positive values
// override, zero/negative keep the documented default.
func TestWithReferenceManifestBudget(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", WithReferenceManifestBudget(1234))
	if r.referenceManifestBudget != 1234 {
		t.Fatalf("budget = %d, want 1234", r.referenceManifestBudget)
	}
	r = NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", WithReferenceManifestBudget(-5))
	if r.referenceManifestBudget != 0 {
		t.Fatalf("budget = %d, want 0 (default selected at render time)", r.referenceManifestBudget)
	}
	if DefaultReferenceManifestBudget != 6000 {
		t.Fatalf("DefaultReferenceManifestBudget = %d, want 6000", DefaultReferenceManifestBudget)
	}
}

// TestComposeSnapshot_ReferenceManifestWithFixtureDocs is task 5.1's verify:
// fixture documents through the real service (fake stores + fake storage),
// manifest rendered by the runner, composed by the real composer — the
// composed instruction carries the manifest block with each visible
// document's entry, after the memory docs.
func TestComposeSnapshot_ReferenceManifestWithFixtureDocs(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-snapshot")
	doc := h.uploadDoc(t, "twilio-api.md", []string{h.atlasID}, nil)
	if _, err := h.svc.UpdateMeta(h.ctx, h.wsID, doc.ID, "twilio-api.md", "The Twilio API manual"); err != nil {
		t.Fatalf("update meta: %v", err)
	}

	docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: h.atlasID})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	r := &Runner{references: h.svc}
	manifest := r.renderReferenceManifest(h.ctx, &agentConfig{
		WorkspaceID:           h.wsID,
		documentSearchEnabled: true,
		visibleDocuments:      docs,
	})

	instruction, err := NewInstructionComposer().Compose(h.ctx, ComposeParams{
		AgentDir:          t.TempDir(),
		MemoryDocs:        []string{"## Memory prefetch\n\nPrefetched memory candidates."},
		ReferenceManifest: manifest,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// The manifest block with the document entry: name, description, and the
	// TOC projection from the real index.
	for _, want := range []string{
		"## Reference documents",
		"- twilio-api.md — The Twilio API manual",
		"TOC: Setup; Webhooks",
		"document.search",
		"document.read",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("composed instruction missing %q:\n%s", want, instruction)
		}
	}
	// Placement: after the memory docs, at the end of the tier.
	if strings.LastIndex(instruction, "Prefetched memory candidates.") > strings.Index(instruction, "- twilio-api.md") {
		t.Errorf("manifest must follow the memory docs:\n%s", instruction)
	}
}

// TestComposeParams_ReferenceManifestPlacedAfterMemoryDocs pins the composer
// placement (5.1): the manifest renders as its own document block after the
// memory docs — and an empty manifest composes no entries. The embedded base
// prompt (task 7.1) carries its own usage-teaching section by the same title,
// so assertions target the manifest block's ENTRY lines, not the title alone.
func TestComposeParams_ReferenceManifestPlacedAfterMemoryDocs(t *testing.T) {
	ctx := context.Background()
	composer := NewInstructionComposer()
	const (
		manifest     = "## Reference documents\n\n- manual.pdf — the manual — TOC: Intro"
		manifestMark = "- manual.pdf — the manual"
	)

	placed, err := composer.Compose(ctx, ComposeParams{
		AgentDir:          t.TempDir(),
		MemoryDocs:        []string{"## Memory prefetch\n\nMEMORY-DOC-MARKER"},
		ReferenceManifest: manifest,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	manifestIdx := strings.Index(placed, manifestMark)
	memoryIdx := strings.Index(placed, "MEMORY-DOC-MARKER")
	if manifestIdx < 0 || memoryIdx < 0 {
		t.Fatalf("expected both the memory doc and the manifest entries:\n%s", placed)
	}
	if memoryIdx > manifestIdx {
		t.Fatalf("manifest must follow the memory docs:\n%s", placed)
	}
	// The manifest block rides verbatim, entries exactly once.
	if strings.Count(placed, manifestMark) != 1 {
		t.Fatalf("expected exactly one manifest entry line:\n%s", placed)
	}

	without, err := composer.Compose(ctx, ComposeParams{
		AgentDir:   t.TempDir(),
		MemoryDocs: []string{"## Memory prefetch\n\nMEMORY-DOC-MARKER"},
	})
	if err != nil {
		t.Fatalf("compose without manifest: %v", err)
	}
	if strings.Contains(without, manifestMark) {
		t.Fatalf("empty manifest must compose no entries:\n%s", without)
	}

	// The trimmed unattended profiles omit the tier with the memory docs —
	// the manifest is never composed into them even when resolved.
	trimmed, err := composer.Compose(ctx, ComposeParams{
		AgentDir:          t.TempDir(),
		ReferenceManifest: manifest,
		SchedulerProfile:  true,
	})
	if err != nil {
		t.Fatalf("compose scheduler profile: %v", err)
	}
	if strings.Contains(trimmed, manifestMark) {
		t.Fatalf("scheduler profile must omit the manifest tier:\n%s", trimmed)
	}
}
