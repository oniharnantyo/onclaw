// Package references implements the deterministic section index for
// workspace reference documents (add-reference-documents design D1–D3).
//
// At upload, a document's original bytes are converted to markdown in-process
// via tools.ConvertDocument (text formats md/txt pass through unconverted),
// then Sectionize splits that markdown into addressable sections — one
// (heading, locator, locator_kind, ordinal, body) tuple each — that a later
// worker indexes into document_sections for full-text search. The sectioner
// is format-agnostic by design (D2): it keys on converter-emitted markdown
// structure — PDF page anchors, pptx slide markers, xlsx sheet markers, ATX
// headings — never on native file formats or content heuristics.
//
// Section bodies are search text: they include their heading line, have page
// anchors stripped, trailing whitespace trimmed, and runs of 3+ newlines
// collapsed to 2. Locators stay verifiable against the original artifact
// (D3): "p. 30" for PDF pages, "slide 5" for pptx, "sheet Users" for xlsx,
// the heading text for docx/md/html, and none for txt/csv and synthetic
// preambles. Sectionize is total: it never returns an empty slice — a wholly
// empty document yields one empty-body section.
package references
