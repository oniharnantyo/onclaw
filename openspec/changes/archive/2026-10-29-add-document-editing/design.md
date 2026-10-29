# Design

## Context

See proposal.md — Why. The load-bearing facts of the current implementation: `create_document` renders markdown to styled PDF through picoloom on go-rod headless Chrome (`internal/agents/tools/docgen`, presets store + tool settings + turn-context appendix already exist); the jailed filesystem backend (`fs_jailed_backend`) already serves images and PDFs multimodally on `read_file`; the tool gate offers catalog tools by global enablement × agent allowlist; seeded skills (`knowledge-search`) are a solved pattern — `SkillName`/`skillContent` consts written to `~/.buddies/skills/<name>/SKILL.md` at startup, repair-if-deleted, shadowable per agent. buddies is an always-on tray gateway: a crash in the agent process takes down every session and the scheduler, so hostile-document safety is a design constraint, not a detail.

## Goals / Non-Goals

**Goals:**
- Revise-in-place with fidelity: a staged DOCX/XLSX/PPTX is changed only where the operation targets.
- One coherent document story: see → stage → edit/regenerate, all through existing jail/gate/artifact machinery.
- Permissive-licensed dependencies only; nothing external to install on user machines.

**Non-Goals:**
- Rewriting PDF text in place (regeneration is the path).
- Creating new PPTX slide layouts (filling existing ones only).
- A config-driven DOCX styling engine (config presets parametrize one built-in default template; bespoke looks come from uploaded templates).
- Changing the attachments pipeline's delivery semantics (attachments still reach the model as message parts).

## Decisions

### D1 — Built-in tools, not an MCP server

New tools live in `internal/agents` beside `create_document`. They need five harness internals an out-of-process server would have to re-invent across the protocol boundary: the workspace jail (path rooting), the preset store and turn-context appendix, artifacts dir-diff chips, the per-agent tool gate, and tool settings. The one genuine MCP win — crash isolation for hostile binaries — is handled in-process (D9). Alternatives rejected: a bundled MCP server (integration loss, two-binary distribution); ZCode's skills+scripts pattern (Python availability is not guaranteed on consumer machines).

### D2 — Dedicated libraries, not a hand-rolled OOXML core

DOCX via `mmonterroca/docxgo/v2` (zero transitive deps, round-trip style preservation, placeholder/mail-merge machinery), PPTX via `kenny-not-dead/gopptx` (shape-level read/edit/save), XLSX via `xuri/excelize/v2`, PDF structural ops via `pdfcpu/pdfcpu` (imported through `pkg/api` only — its CLI dependencies sit in go.mod but never link into the binary). All go 1.23/1.24+ floors against the repo's go 1.26. Alternatives rejected: stdlib `archive/zip`+`encoding/xml` (the knowledge-corpus recorded fallback — more control but we own all XML grinding and fidelity edge cases); unioffice (AGPL/commercial, violates the free-library constraint).

### D3 — License verification is a landing gate

docxgo's LICENSE file is not a standard MIT text (GitHub reports NOASSERTION) and its history descends from fumiama/go-docx's AGPL era; the README claims a provenance audit. Before merge: read `LICENSE` and `CREDITS.md`, record the verdict. If it fails review, the D2 fallback is the stdlib core behind the same tool seams.

### D4 — Seeing = decode-at-read in the jailed read path

`read_file` transparently renders DOCX/PPTX as structured text views and XLSX as cell-grid views; the file is never modified and nothing is written back. The view is the model's eyes, not an export — there is no markdown copy that could drift from the original. Views are paginated like text reads so large workbooks do not flood context. Alternatives rejected: a separate view tool (a second verb for what read already means); markdown roundtrip (lossy, drifts).

### D5 — Editing = targeted operations against the original

The edit tool applies element-level operations (replace/insert/delete at addresses named by the D4 views; placeholder fills) and writes the result preserving everything untouched — docxgo's round-trip guarantee is the DOCX mechanism. Placeholder fills ride docxgo's merge machinery where possible, which is expected to handle Word's run-splitting of `{{tokens}}`; if verification shows otherwise, runs are pre-merged before matching. Rejected operations and processing failures return `{ok:false}`-style results; a Go error is only for infrastructure failures — the create_document error convention.

### D6 — Generation into templates

`create_document` gains DOCX and PPTX output selected by filename extension and preset availability. Template-document presets are user-uploaded skeletons; generation copies the skeleton and injects content (placeholder fill; body content into the styled body region for DOCX; existing text placeholders for PPTX). DOCX config presets parametrize one shipped built-in default template — parameters override values in its styles; this is deliberately not a styling engine. Presets store and Tools pane dialog grow a format dimension (per-format defaults, per-format tabs).

### D7 — PDF boundary and sequencing

The PDF tool does structural work only (merge, split/extract, rotate, stamp/watermark, form fill) via pdfcpu. Text rewriting is refused with a result pointing at create_document regeneration. Hard prerequisite, landing before any PDF-facing task is smoke-tested: the provider bug that turn-kills on PDF file parts (open since the Oct-25 knowledge-corpus smoke) — a separate fix, tracked outside this change.

### D8 — Staging (superseded during apply)

Removed by user decision on 2026-10-29: there is no stage_document tool. Office chat attachments reach the agent as reference notes; save_document can copy one into the knowledge scope, where read_file renders the office view. Knowledge documents stay read-only — revisions happen through regeneration. The workspace-rooted tool jail is unchanged.

### D9 — Hostile-document safety in-process

Ingestion's 20 MiB cap already bounds input size; zip-member decompression is capped while reading (defeats zip bombs); Go's `encoding/xml` does not resolve external entities; every document tool wraps its body in `recover()` so a library panic becomes a readable tool result, not a gateway crash. The heaviest renderer (Chrome, for PDF generation) already runs in its own process.

### D10 — The `documents` skill rides the seeded-skill pattern

`SkillName`/`skillContent` consts in the new document tools package, seeded repair-if-deleted into the global skills tier at startup — discovery, gating, `$documents` invocation, and agent-tier shadowing come free. Directed activation via the frontmatter description (the knowledge-search mechanism); no heuristic middleware.

### D11 — Early-stage dependency behind a narrow seam

gopptx (2 stars, no releases) is pinned to an exact version and touched only through a small engine interface (the `docgenEngine` precedent), so a swap to the stdlib OOXML path — or a successor library — is a package-local change. Tool names (`edit_document`, `stage_document`, PDF ops) live at the same seams and may shift during apply without touching the specs.

## Apply Revisions (2026-10-29, user decisions)

1. **edit_pdf merged into edit_document.** One invokable tool dispatches on the file's extension: office files take the targeted in-place ops (D5), PDFs take the structural ops (D7) in the same ops array, each PDF operation naming its own inputs and output. D11's naming freedom made this an apply-time change; the spec requirements are capability-level and unchanged. Rationale: one fewer tool in every document-interested agent's catalog and allowlist.
2. **stage_document removed** (D8 superseded, above). The documents skill's playbook and the office-attachment turn note were rewritten to match: attachments view via save_document's knowledge-scope copy; knowledge documents revise through regeneration.

## Risks / Trade-offs

- [docxgo provenance ambiguity] → D3 license gate before merge; stdlib fallback behind the same seams.
- [gopptx bus-factor one] → exact pin + D11 seam; PPTX scope (text in existing shapes) keeps swap cost low.
- [Round-trip fidelity claims unverified on real files] → golden-file tests: apply an edit, assert untouched regions' XML is byte-identical; include Word-, Pages-, and LibreOffice-authored fixtures.
- [PDF file-block provider bug blocks PDF smokes] → D7 sequencing: PDF-facing tasks gated on that fix landing first.
- [Large documents flood the context through views] → view pagination with read-style offsets; grid views list populated cells only.
- [Tool-surface growth burdens every turn's token budget] → the existing gate already keeps niche tools out of uninterested agents' allowlists; the seeded skill pays its tokens only on document intent.

## Migration Plan

Additive throughout: new catalog entries, new preset rows (format column is additive), widened acceptance in the knowledge gate. No data migrations; the presets store gains rows, not schema breaks. Rollback is revert — worst case, users see the old rejection copy for OOXML uploads again. Generated artifacts already on disk stay valid.

## Open Questions

None that block tasks. The built-in default DOCX template's visual design is an apply-time artifact to review with the user, not an approach question.

## D3 License Verdict (apply record, 2026-10-28)

docxgo v2.14.0 **passes** the D3 gate. `LICENSE` in the module is a standard
MIT text with explicit MIT attribution to the predecessor authors; `CREDITS.md`
discloses the full genealogy including the fumiama/go-docx AGPL period, and the
project's own completed provenance audit (`docs/PROVENANCE_AUDIT.md`, audited
2026-07-18 against the v2.7.1 baseline) determines the current rewritten
release contains no protectable AGPL implementation — the AGPL era is confined
to historical snapshots. No stdlib fallback needed. Resolved pins: docxgo
v2.14.0, gopptx v0.7.0 (a tagged release exists; pinned exact per D11), excelize
v2.11.0, pdfcpu v0.16.4.
