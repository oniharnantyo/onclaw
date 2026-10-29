# Tasks

## 1. Backend — office views in the jailed read path (spec document-editing: Reading office documents)

- [x] 1.1 `internal/agents/backend/`: decode-at-read views for OOXML — DOCX and
  PPTX render as structured text views (paragraphs/headings, slide/shape text in
  document order), XLSX as a cell-grid view (sheets with populated cells and
  addresses); the file is never modified (byte-for-byte) and views are paginated
  like text reads; view lines carry addresses (`¶n` for DOCX blocks, `slide n,
  shape m` for PPTX, `Sheet!Cell` for XLSX) precise enough to target in
  edit_document operations.
- [x] 1.2 Tests: fixtures authored by the same libraries (docxgo/excelize/gopptx),
  unchanged-file assertions, pagination cases, non-office files unaffected.

## 2. Backend — documents package: OOXML edit engines + documents skill (spec document-editing: In-place revision / Documents skill)

- [x] 2.1 New `internal/agents/tools/documents/`: targeted edit engines — DOCX
  via docxgo (replace/insert/delete paragraph at `¶n`, placeholder fill),
  XLSX via excelize (set/clear cell, insert/delete row), PPTX via gopptx
  (replace shape text at `slide n, shape m`); every op preserves untouched
  content and styling; rejected operations and processing failures return
  readable `{ok:false}`-style results, Go errors only for infrastructure.
- [x] 2.2 Safety: 20 MiB input cap, zip pre-scan capping total decompressed
  size, `recover()` around op application (D9).
- [x] 2.3 Golden-file fidelity tests: apply an edit, assert untouched content
  (text + styles) unchanged on re-read; Word-authored-style fixtures covered by
  library-authored equivalents.
- [x] 2.4 `documents` skill seeding in the same package (`SkillName`,
  `skillContent`, `SeedDocumentsSkill`, repair-if-deleted, mirroring
  `knowledge.SeedSkill`): description states it applies to reading, revising,
  filling, or producing office documents and manipulating PDFs; body teaches
  the playbook (view before edit, edit workspace files — attachments view
  through save_document's knowledge-scope copy, knowledge documents revise
  through regeneration — targeted ops over regeneration,
  presets via create_document, PDF text changes → regeneration).

## 3. Backend — pdfops package: PDF structural operations (spec document-editing: PDF structural operations)

- [x] 3.1 New `internal/agents/tools/pdfops/`: merge, split/extract page
  ranges, rotate pages, stamp/watermark, form fill — pdfcpu imported through
  `pkg/api` only; inputs unchanged, explicit output paths.
- [x] 3.2 Text-rewrite requests return a readable result explaining PDF text
  changes are made by regenerating the document (naming create_document);
  `recover()` + 20 MiB cap; `{ok:false}` results for rejected ops.

## 4. Backend — docgen formats: DOCX/PPTX generation, template + config presets (spec document-generation: Document presets)

- [x] 4.1 `internal/store/documentpresets.go`: format vocabulary constants
  (docx, pptx alongside pdf); new `internal/store/documenttemplates.go`:
  on-disk template store (save/path/delete) rooted at the app data dir —
  preset rows gain no schema changes.
- [x] 4.2 `internal/agents/tools/docgen/`: DOCX engine (template flavor —
  copy skeleton, inject body content + placeholder fill; config flavor —
  built-in default template generated at runtime via docxgo, parameters
  override fonts/heading/page/footer) and PPTX engine (template flavor only —
  fill existing text placeholders, never invent layouts); `create_document`
  output format selected by filename extension.
- [x] 4.3 `internal/agents/docgen_tool.go` + `internal/agents/contract.go` +
  `runner.go`: per-format preset resolution and the turn-context appendix
  listing presets per format with per-format defaults; PPTX generation
  without a template returns a readable error.
- [x] 4.4 `internal/services/documentpresets.go`: template upload/list
  (has-template flag) and delete-cleans-template service methods; deleting
  the default preset resets that format's default selection (existing
  behavior extended per format).
- [x] 4.5 Tests: engines (fixture round-trips), store (template lifecycle),
  service (upload validation), appendix rendering.

## 5. Backend — tools, wiring, knowledge gate (spec document-editing: office revision + PDF structural ops / knowledge delta)

- [x] 5.1 `internal/agents/docedit_tool.go`: the invokable `edit_document`
  tool over the documents and pdfops packages — office revisions in place,
  PDF structural operations riding the same ops array with per-operation
  inputs/outputs, and the `rewrite_text` regeneration redirect. (Revised
  during apply, 2026-10-29, user decisions: `edit_pdf` merged into
  `edit_document`; `stage_document` removed.)
- [x] 5.2 `internal/agents/builtin_tools.go`: catalog entries for
  `edit_document` (office + PDF structural, the no-text-rewrite rule in its
  description) and `create_document`; wiring in the builtin tool factory.
- [x] 5.3 `internal/cli/app.go`: seed the `documents` skill beside the
  knowledge skill seeding.
- [x] 5.4 `internal/knowledge/knowledge.go`: ingestion gate accepts OOXML
  (.docx/.xlsx/.pptx, 20 MiB) — kind classification and rejection copy name
  text, PDF, and OOXML as supported; rejections still create no rows/files.
- [x] 5.5 `internal/attachments/attachments.go`: OOXML lane (20 MiB);
  rejection copy updated; office attachments ride turns as non-multimodal
  reference notes pointing at save_document's knowledge-scope copy path.
- [x] 5.6 Tests: tools (workspace rooting, in-place edits, PDF ops with
  unchanged inputs), gate (accept/reject/oversize per kind), lanes; `go vet
  ./...`, `go test ./...` green.

## 6. Frontend — Tools pane per-format tabs + copy (spec document-generation: Preset management in the Tools pane; knowledge rejection copy)

- [x] 6.1 `frontend/src/features/settings/components/DocumentConfigDialog.tsx`:
  one tab per format (PDF, DOCX, PPTX), each listing that format's presets
  with create/edit/duplicate/delete controls and the format's default
  selector; legible in both themes.
- [x] 6.2 DOCX tab hosts the config-preset editor grouped by concern (fonts,
  heading style, page setup, footer) and the template upload control; PPTX
  tab hosts the template upload; saving is atomic.
- [x] 6.3 Knowledge surfaces' rejection copy names OOXML documents as
  supported.
- [x] 6.4 Regenerate bindings (`task common:generate:bindings`); store/service
  tests updated; `vp check src` green.
