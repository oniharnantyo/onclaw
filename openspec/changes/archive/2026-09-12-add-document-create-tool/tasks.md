## 1. Tool shell + family registration

- [x] 1.1 Implement the `document.create` tool shell: `document.create({path, format, template?, data})` — output path validated through the jail's write contract (same rules as `write_file`), format dispatching to a per-format generator registry, structured per-document error results (run continues).
- [x] 1.2 Register the verb through the `document.*` family seam: catalog entry (group `document`, icon), `document.create` permission key subject to the workspace tool gate, hook target addressable by name.
- [x] 1.3 Tests: path validation (workspace-writable OK, jail escape rejected), unknown-format rejection, per-format dispatch, structured error results.

## 2. Default generators (no template)

- [x] 2.1 xlsx generator: `excelize/v2` from structured sheet/row input, basic cell styles; tests with generated fixtures.
- [x] 2.2 docx generator: stdlib `archive/zip`+`encoding/xml` from markdown input (headings, paragraphs, bold/italic, tables); tests with generated fixtures.
- [x] 2.3 pptx generator: embed a blank skeleton deck (one master/layout/theme) in the binary, clone per call, inject `{slides: [{title, bullets, notes}]}` text shapes; tests asserting a valid output zip opens with correct slide count.
- [x] 2.4 pdf structured generator: `fpdf` invoice renderer (seller, buyer, line items, tax, totals); tests with fixture data.

## 3. PDF HTML route (rod)

- [x] 3.1 Implement `data.source: "html"`: render agent-supplied HTML/CSS to PDF via rod headless Chrome; bounded render deadline.
- [x] 3.2 Chrome-absent degradation: structured per-document error naming the limitation and pointing at the structured route; tool remains selectable.
- [x] 3.3 Tests: HTML→PDF smoke (valid PDF bytes out), degradation path with rod unavailable (injected failure).

## 4. Template overlay

- [x] 4.1 `template` parameter: jail-validated path accepted from the run's drop-lane mount or workspace files; absent → default generation.
- [x] 4.2 Template-fill: xlsx (named cells/ranges via excelize), docx and pptx (`{{placeholder}}` replacement with XML run-merging); PDF template attempts rejected with guidance (final-form format).
- [x] 4.3 Tests: split-run placeholder fixture (`{{na`+`me}}` across `<w:r>`), xlsx cell fill, pptx template fill, PDF-template rejection copy.

## 5. Delivery

- [x] 5.1 Post-creation delivery: copy output into workspace storage, obtain capability URL, stamp it on the tool result; tool card renders the download link.
- [x] 5.2 Web: creation sentence in `toolDisplay.ts` ("Creating document X" → "Created document X", format fact + latency), catalog mirror entries, download-link affordance on `document.create` cards.

## 6. Verification

- [ ] 6.1 `go build ./...`, `go vet ./...`, touched suites green; smoke suite extended for `document.create` (create → capability URL serves bytes → card link present).
- [ ] 6.2 Manual pass: agent creates an invoice (PDF structured), an invoice from an HTML route, a deck, and an xlsx from a chat-attached template; user downloads each from the transcript card.
