# Proposal

## Why

The document pipeline is one-way: `create_document` renders markdown into styled PDF, but agents cannot read office documents (DOCX/XLSX/PPTX are rejected at every ingestion path) and cannot revise a document the user already has. Users must hand-edit files their agents could handle. The v1 decisions left this room open deliberately — presets were scoped "PDF only in v1", and the knowledge-corpus design recorded "decode-at-read via Go stdlib archive/zip + encoding/xml if OOXML ever returns".

## What Changes

- **Built-in editing tools** in `internal/agents`, beside `create_document`: in-place revision of DOCX/XLSX/PPTX files via targeted, element-level operations that preserve all untouched content and styling, and a PDF structural-operations tool (merge, split, stamp/watermark, rotate, form-fill). PDF text is never rewritten in place — text changes route through regeneration via `create_document`.
- **Seeing office documents**: `read_file` transparently presents DOCX/PPTX as text views and XLSX as a grid view (the view is the model's eyes only, never an export); the knowledge ingestion gate widens to accept OOXML documents alongside text and PDF.
- **Generation into templates**: `create_document` gains DOCX and PPTX output. Presets gain a template-document flavor — the user uploads a styled skeleton and the agent's content is injected into a copy — plus DOCX config presets that parametrize the shipped built-in default template. The Tools pane dialog grows per-format tabs.
- **A seeded `documents` skill** in the global skills tier (knowledge-search precedent): teaches when to see vs edit vs regenerate, targeted-op discipline, and the PDF text-changes-regenerate rule. Shadowable per agent.
- **New dependencies** (all permissive licenses; unioffice/AGPL deliberately avoided): `mmonterroca/docxgo/v2`, `kenny-not-dead/gopptx`, `xuri/excelize/v2`, `pdfcpu/pdfcpu`.

Prerequisite (separate fix, not in this change): the provider bug that turn-kills on PDF file parts must be resolved before any PDF-facing behavior can work end to end.

## Capabilities

### New Capabilities

- `document-editing`: agents see and revise office documents in place with fidelity, apply structural operations to PDFs through the same editing tool, and follow a seeded `documents` playbook skill.

### Modified Capabilities

- `knowledge`: the document format gate changes from "plain text and PDF only" to also accepting OOXML documents (DOCX/XLSX/PPTX); word-processing/spreadsheet/presentation kinds move from the rejected list to the supported list.
- `document-generation`: document presets are no longer PDF-only — the spec grows DOCX (template + config flavors) and PPTX (template flavor) presets, per-format tabs in the Tools pane dialog, and `create_document` output beyond PDF.

## Impact

- `internal/agents`: new tools + catalog entries + gate wiring; `internal/agents/tools/docgen` gains formats/preset flavors; new OOXML/pdfcpu tool packages.
- `internal/knowledge`: ingestion gate accepts OOXML; seeded skill.
- `internal/agents/backend`: `read_file` decode-at-read views for OOXML.
- Frontend: Tools pane preset dialog tabs; knowledge surfaces' rejection copy; bindings regeneration.
- `go.mod`: four new libraries. PDF file-block provider bug is a sequencing prerequisite.

Out of scope: rewriting PDF text in place; creating new PPTX slide layouts; unioffice or any AGPL/commercial-licensed library; external binaries (LibreOffice, pandoc, Python).
