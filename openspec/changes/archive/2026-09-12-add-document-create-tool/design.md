## Context

`add-document-read-tool` locked the creation direction as a family seam: `document.*` verbs register with catalog metadata, a per-verb permission key, a tool-card rule, and a hook target — no new plumbing per verb. This change is the first consumer of that promise.

The exploration (2026-09-12) locked four user decisions: invoice formats are **both PDF and xlsx**; templates are **user-optional** and arrive **by chat**; the rod HTML→PDF route is **in v1**; there is **no mandatory template workflow** and no workspace-assets management surface.

Pure-Go generation feasibility drove the format set: xlsx (`excelize/v2`) and PDF (`fpdf`) generate from data natively; docx and pptx are OOXML zips the stdlib can write (docx directly, pptx via an embedded blank skeleton to avoid authoring slide-master/theme XML by hand).

## Goals / Non-Goals

- Goals: one `document.create` verb serving xlsx, pdf, docx, pptx; optional user templates with chat-attachment delivery; created documents downloadable from the transcript; zero mandatory setup.
- Non-Goals: a template-management UI or workspace-assets concept (revisit only if chat-delivered templates prove limiting); PDF template-fill (AcroForm — final-form format); charts/images in v1 decks; complex docx styling (styles.xml authoring beyond headings/tables); editing existing documents (that is a later `document.edit` if ever).

## Decisions

- **D1 — One tool, structured per-format input (user-locked direction).** `document.create({path, format, template?, data})`. Input dialect follows the format's natural shape: docx takes markdown; pptx takes `{slides: [{title, bullets, notes}]}`; xlsx takes sheet/row structures; pdf takes one of two source modes (D4). Structured input, not freeform blobs, because creation is lossless-intent work and the model is better at filling a schema than emitting valid OOXML.
  - *Alternative considered:* markdown-in for everything — rejected: markdown cannot express slide structure or invoice totals boxes.
- **D2 — Default generation is from-scratch, pure Go (user-locked: templates optional).** xlsx → `excelize/v2`; docx → stdlib `archive/zip`+`encoding/xml` (headings, paragraphs, inline bold/italic, tables); pptx → embedded blank skeleton (one master/layout/theme compiled into the binary, cloned per call, text shapes injected); pdf → D4's routes. The embedded skeleton is invisible infrastructure, not a template workflow — the user never manages it.
  - *Alternative considered:* authoring pptx masters/layouts/theme XML from zero — rejected: worst effort-to-value in the feature; the skeleton bounds v1 decks to a clean house style, which is the honest scope of pure-Go generation anyway.
- **D3 — Templates are an optional overlay delivered by chat (user-locked).** `template` is an optional jail-validated path: the run's drop-lane mount (user attached the template in chat) or an ordinary workspace file. Present → template-fill (clone template, inject data at placeholders); absent → D2's default generation. Fill mechanics: xlsx writes named cells/ranges via excelize; docx and pptx replace `{{placeholder}}` text after merging XML runs (Word splits `{{na|me}}` across `<w:r>` runs — a known problem with a known fix); **PDF is excluded from template-fill** — invoice templates are xlsx or docx, PDF invoices are always generated.
  - *Known limitation:* chat-delivered templates are run-scoped — the mount dies with the run. Reuse across sessions means re-attaching, or copying into the workspace via the shell tool when enabled. A "save template to workspace" affordance is a later change if the friction is real.
- **D4 — PDF is dual-route (user-locked: both formats, rod in v1).** `data.source: "html"` → agent-authored HTML/CSS rendered to PDF via rod headless Chrome — bespoke, invoice-grade styling with no template asset; `data.source: "structured"` → invoice schema (seller, buyer, line items, tax, totals) rendered by pure-Go `fpdf` — always available, no Chrome. When Chrome is absent, the HTML route returns the same structured per-document error pattern the browser tool uses; the structured route keeps the tool useful on minimal deployments.
  - *Alternative considered:* rod-only — rejected: it would tax deployments that never enabled browser tools with a Chrome requirement for a core invoice path.
- **D5 — Output path validation reuses the jail's write contract.** The output path resolves through the same rules as `write_file` (primary root and writable mounts only). Created files land in the agent workspace; the tool never writes outside the jail.
- **D6 — Delivery rides the capability-URL machinery.** After writing the file, the tool copies it into workspace storage, requests a capability URL, and stamps it on the tool result. The transcript tool card renders a download link from that URL — the same serving path attachments already use. No new download endpoint, no new UI component beyond the link affordance.
- **D7 — Family registration only (the seam pays off).** The verb registers with catalog metadata (group `document`, icon), a `document.create` permission key under the workspace tool gate, a creation-verb sentence in the tool-card table ("Creating document X" → "Created document X" with format + latency), and a hook target. Adding `document.export` or similar later is one more registration.

## Risks / Trade-offs

- [PPTX v1 decks are house-style only (title + content slides, one theme)] → Accepted; the skeleton can be restyled by swapping the embedded asset. Rich decks need a template, which D3 already supports.
- [docx/pptx template-fill depends on placeholder run-merging correctness] → Known OOXML problem with a bounded fix; test fixtures pin the split-run case.
- [Agent-authored HTML quality varies] → It is also rod's strength (bespoke output); the structured fpdf route is the deterministic fallback. Raw toggle on the tool card lets users inspect either.
- [Token cost of HTML/slides in tool args] → Bounded by the same reasoning as any write_file: the model pays tokens for output it intends to produce; caps mirror the read tool's output thinking only if abuse appears.
- [Capability-URL copy adds a storage write per creation] → Negligible; reuses the local/S3 drivers transparently.

## Migration Plan

No DB migration. Deploy order: after `add-document-read-tool` (capability + mount contract prerequisites). Rollback is a revert; created files in workspaces and their transcript cards remain but new creations stop.

## Open Questions

- xlsx input shape (JSON sheet/row structures vs markdown-table shorthand) — settle at implementation against real model behavior; the spec fixes only that input is structured and cell-level styles are reachable.
- Embedded skeleton's visual design (fonts, accent color) — a one-asset decision at implementation, aligned with the design contract's tokens.
