## 1. Lane reclassification

- [x] 1.1 Reclassify uploads in `internal/attachments/attachments.go`: PDFs → drop lane; docx/xlsx/pptx → drop lane; reject lane shrinks to legacy doc/ppt binaries, archives, executables, unknowns (update domain constants and error copy).
- [x] 1.2 Retire the inline-pdf lane in `internal/agents/attachments_message.go` and `internal/domain/attachment.go`: drop-lane pointer note for documents names `document.read`; historical persisted `inline-pdf` lane values keep rendering (D7 — runner treats legacy refs as pointer-style).
- [x] 1.3 Tests: classification matrix (pdf/docx/xlsx/pptx → drop; doc/ppt → reject; images/text unchanged), pointer-note copy names the tool, legacy lane round-trip renders.

## 2. document.read tool

- [x] 2.1 Implement the `document.read` tool: path validation via the jailed backend contract, in-process conversion dispatch through a per-format converter registry with a bounded conversion deadline, output cap + truncation notice, distinct empty-text result, structured per-document conversion-failure result.
- [x] 2.2 Implement the pure-Go converters and add their dependencies: pdf (`ledongthuc/pdf`), xlsx (`excelize/v2`), docx + pptx (stdlib `archive/zip` + `encoding/xml` over `word/document.xml` / `ppt/slides/slideN.xml`), html (`x/net/html` promoted direct), csv/tsv (stdlib `encoding/csv`) — each registered into the format registry.
- [x] 2.3 Tests: happy path per format (pdf/docx/xlsx/pptx/html/csv fixtures generated in-test), path-traversal rejection, truncation, scanned-PDF emptiness, corrupt-document conversion failure, conversion deadline.

## 3. Tool family plumbing

- [x] 3.1 Register `document.read` in the catalog/tool registry under the `document.*` family: stable key, display name, description, group, icon key, per-verb permission key wired to the workspace tool gate.
- [x] 3.2 Add the tool-card rendering rule for the family (readable verb + document name + latency) and the hook target.

## 4. Verification

- [x] 4.1 `go build ./...`, `go vet ./...`, touched suites green; smoke suite updated for the new upload classification.
- [ ] 4.2 Manual pass: upload docx/xlsx/pptx/PDF → drop-lane chips; agent reads each via `document.read` (markdown in transcript tool card); `/compact` a session with documents; OpenAI-family agent with a PDF attachment runs clean end-to-end.
