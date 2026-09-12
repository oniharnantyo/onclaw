## Why

Agents can read documents but cannot produce them. Users ask agents for deliverables — pitch decks, invoices, reports — and today the only answer is a wall of markdown the human must format by hand. The `document.*` family seam landed with `document.read` (registration, catalog entry, permission key, tool-card rule, hook target) precisely so a creation verb would need no new plumbing. This change supplies that verb.

## What Changes

- **New built-in tool `document.create`**: input is an output path (jail-writable, same contract as `write_file`), a format, and format-specific structured data. Supported formats at launch:
  - **xlsx** — generated with `excelize/v2` (sheets/rows/styles from structured data);
  - **pdf** — two routes: agent-authored HTML rendered via **rod** headless Chrome (bespoke styling, the invoice-grade route), or structured invoice data rendered via pure-Go `fpdf` (Chrome-free fallback, always available);
  - **docx** — hand-rolled OOXML (`archive/zip` + `encoding/xml`): headings, paragraphs, bold/italic, tables from markdown input;
  - **pptx** — an embedded blank skeleton (one master/layout/theme, authored once and compiled into the binary) cloned per call; the agent supplies `{slides: [{title, bullets, notes}]}` and the tool injects text shapes.
- **Optional template overlay (user-locked)**: a `template` path parameter switches from default generation to template-fill. Templates arrive **by chat** — a drop-lane attachment already mounted read-only into the run — or as ordinary workspace files; both are the same jail path contract. PDF has no template-fill (final-form format): invoice templates are xlsx or docx.
- **Delivery back to the user**: the created file lives in the agent workspace; the tool copies it into workspace storage and stamps a capability URL on the result, so the transcript tool card renders a download link using existing attachment-serving machinery.
- **Family registration only**: catalog entry (`document.create`, group `document`), per-verb permission key subject to the workspace tool gate, tool-card sentence (creation verb + document name + latency), and hook target — all through the seam `document.read` built. No new plumbing.

## Capabilities

### Modified Capabilities

- `workspace-document-tools`: ADDED requirements for the creation verb — tool contract (formats, structured input, jail-writable output), template overlay (chat-attachment source, fill semantics, PDF excluded), PDF dual-route with Chrome-absent degradation, and result delivery via capability URL. (The capability itself ships with `add-document-read-tool`; this change stacks on it and lands after it.)

## Impact

- `internal/agents/tools/document_create.go` (new tool + generator registry), `internal/agents/tools/document_skeleton_pptx.go` (embedded skeleton asset), `internal/agents/tool_catalog.go` / `tool_registry.go` (family registration), `internal/domain/permissions.go` (verb permission key), `internal/agents/runner.go` (rod wiring for the HTML route), `web/src/lib/toolDisplay.ts` + `toolCatalog.ts` (creation sentence + catalog mirror), `web/src/components/chat/ToolCall.tsx` (download-link affordance on the card).
- New Go dependency: `github.com/go-pdf/fpdf` (pure Go, no cgo). `excelize/v2` and rod are already entering the module graph via `add-document-read-tool` and the browser tools respectively.
- Deploy order: after `add-document-read-tool` (the `workspace-document-tools` capability and the drop-lane mount path contract must exist first).
- Frontend: tool card gains a download-link rendering for `document.create` results; no new screens.
