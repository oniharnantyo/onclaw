package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// DefaultReferenceManifestBudget is the default total character budget of the
// compose-time reference-documents manifest block (add-reference-documents
// 5.2, design D6): the header plus one entry per visible document, capped so
// a large library degrades detail — never discovery. Override per runner
// with WithReferenceManifestBudget.
const DefaultReferenceManifestBudget = 6000

// WithReferenceManifestBudget overrides the manifest's total character budget
// (add-reference-documents 5.2). Non-positive values select the documented
// default. Overflow collapses later entries' TOCs first (entry becomes
// `- <name> — <description>`), then longer descriptions truncate — entries
// are NEVER dropped, so discovery survives a large library (D6).
func WithReferenceManifestBudget(n int) RunnerOption {
	return func(r *Runner) {
		if n > 0 {
			r.referenceManifestBudget = n
		}
	}
}

// renderReferenceManifest pre-renders the compose-time reference-documents
// manifest (add-reference-documents 5.1/5.2, design D6) from the visibility
// lens resolve() evaluated: one entry per visible document — name,
// description, page (or equivalent) count, top-level table of contents —
// introduced by the library's usage contract. Skipped entirely (empty
// string) when the library is unwired, the run's post-gate effective tool
// set excludes document.search, or nothing is visible: the manifest is
// discovery for the document tools, never prompt decoration. Never fails the
// run — a TOC read error degrades that entry, the same direction budget
// overflow takes.
func (r *Runner) renderReferenceManifest(ctx context.Context, cfg *agentConfig) string {
	if r.references == nil || !cfg.documentSearchEnabled || len(cfg.visibleDocuments) == 0 {
		return ""
	}

	entries := make([]string, 0, len(cfg.visibleDocuments))
	for _, doc := range cfg.visibleDocuments {
		entries = append(entries, r.referenceManifestEntry(ctx, cfg, doc))
	}

	// Budget pass (5.2): entries render at full detail while the block fits;
	// past the budget each later entry collapses — TOC and page count drop
	// first, then the description truncates. The name is never dropped, so
	// the block can marginally exceed the cap only when a single bare name
	// alone would.
	budget := r.referenceManifestBudget
	if budget <= 0 {
		budget = DefaultReferenceManifestBudget
	}
	remaining := budget - len(referenceManifestHeader)
	for i, entry := range entries {
		if len(entry) <= remaining {
			remaining -= len(entry) + 1 // + the joining newline
			continue
		}
		entries[i] = collapseManifestEntry(cfg.visibleDocuments[i], remaining)
		remaining -= len(entries[i]) + 1
	}

	return referenceManifestHeader + strings.Join(entries, "\n")
}

// referenceManifestHeader opens the manifest block: what the library is,
// where it mounts, how to search and read it, and the citation contract
// (5.2's usage line).
const referenceManifestHeader = "## Reference documents\n\n" +
	"The workspace's reference-document library is mounted read-only under references/. " +
	"Search it with document.search; read a document scoped with document.read " +
	"(pages for PDFs, section for heading, slide, or sheet titles). " +
	"Cite what you use by name and locator (page, slide, sheet, or heading), linking references/<document name>.\n\n"

// referenceManifestEntry renders one full-detail manifest entry:
// `- <name> — <description> — <page count> — TOC: <headings>`. The
// page-count segment is omitted when the document reports none (md/txt have
// no pages); the TOC segment is omitted when the index carries no Level<=1
// headings.
func (r *Runner) referenceManifestEntry(ctx context.Context, cfg *agentConfig, doc domain.ReferenceDocument) string {
	segments := make([]string, 0, 4)
	segments = append(segments, doc.Name)
	if desc := strings.TrimSpace(doc.Description); desc != "" {
		segments = append(segments, desc)
	}
	if doc.PageCount > 0 {
		segments = append(segments, fmt.Sprintf("%d pages", doc.PageCount))
	}
	// The TOC projection is every distinct Level<=1 heading in ordinal order
	// (fix-reference-document-retrieval D1): topN 0 is the projection's
	// uncapped convention, so a deep section like 5.1 PostLogin stays
	// reachable through its chapter heading no matter how many earlier
	// mixed-level headings precede it — the old fixed 6-entry mixed-level
	// prefix cut exactly such chapters off. How much of the projection
	// survives is governed by the manifest budget pass below, never by an
	// entry count: long TOCs collapse per the existing per-entry rule.
	if toc := r.documentTOC(ctx, cfg, doc.ID, 0); len(toc) > 0 {
		segments = append(segments, "TOC: "+strings.Join(toc, "; "))
	}
	return "- " + strings.Join(segments, " — ")
}

// documentTOC reads one document's top-level headings through the library's
// TOC projection, scoped to the run's workspace (topN <= 0 requests the
// uncapped projection — every Level<=1 heading in ordinal order). A read
// error degrades to no TOC — the manifest is fail-open (a missing TOC costs
// the agent one document.search, never the run).
func (r *Runner) documentTOC(ctx context.Context, cfg *agentConfig, docID string, topN int) []string {
	toc, err := r.references.TableOfContents(ctx, cfg.WorkspaceID, docID, topN)
	if err != nil {
		return nil
	}
	return toc
}

// collapseManifestEntry degrades one over-budget entry: TOC and page count
// drop first (`- <name> — <description>`); when that still exceeds the
// remaining budget the description truncates with an ellipsis. The name is
// never dropped (D6).
func collapseManifestEntry(doc domain.ReferenceDocument, remaining int) string {
	name := "- " + doc.Name
	desc := strings.TrimSpace(doc.Description)
	if desc == "" {
		return name // the description was empty; there is nothing to collapse
	}
	if len(name)+2 <= remaining {
		budgeted := remaining - len(name) - 2
		if len(desc) <= budgeted {
			return name + " — " + desc
		}
		if budgeted > 1 {
			cut := truncateRuneSafe(desc, budgeted-1)
			return name + " — " + cut + "…"
		}
	}
	return name
}

// truncateRuneSafe cuts s to at most n bytes without splitting a UTF-8 rune.
func truncateRuneSafe(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}
