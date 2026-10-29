# Design

## Context

The turn's model-input assembly gates PDF file parts on two conditions ANDed together: the effective model's stored capability row (`pdf_input`, the provider-settings checkbox, resolved via the capability resolver) and `carriesPDFParts` — a hardcoded `providerType == gemini` check. Connector reality, verified in the pinned eino-ext sources: the claude connector maps base64 file blocks to native PDF document blocks (both in user messages and in tool results); the OpenAI-family chat-completions conversion has no file-block case at all and errors a turn that tries; the gemini connector carries user-message files inline but accepts text-only tool results. So the gemini-only gate is one family too strict (anthropic) and accidentally right for the third (OpenAI family). When degradation does happen, the pointer note exposes only the capability key; the only agent-reachable route (save_document into `/knowledge`) misuses the curated corpus as a landing zone, and `read_file` on a PDF anywhere errors the turn on two of three families (tool-result file parts). The agent's workspace is already the jail's read-write root mounted at `/workspace`, and artifact detection scans only `artifacts/`, so a workspace `uploads/` directory is reachable by existing tools without touching mounts, jail roots, or artifact chips.

See proposal.md for motivation; the spec deltas define the behavioral contract.

## Goals / Non-Goals

**Goals:**

- The provider-settings checkbox decides PDF carriage wherever a connector can actually carry the part.
- Degraded attachments leave the agent a working path to the bytes: workspace `uploads/`, readable with existing filesystem tools.
- No turn fails because a kind could not be carried — as a message part or as a tool result.
- One per-(kind, family) carriage matrix is the single source of truth for the message lane, the tool-result lane, and the composer warning.

**Non-Goals:**

- Wiring or removing the dead `SupportsResponsesAPI` (a separate route to OpenAI-family PDFs; follow-up).
- Gemini's inability to carry *images* in tool results (pre-existing adjacent gap; unchanged here).
- Video carriage beyond gemini; upload size caps; uploads cleanup/retention policy.
- Honoring the PDF page-range clause of the existing multimodal-read requirement (pre-existing implementation drift, unchanged).

## Decisions

**D1 — Carriage matrix replaces the gemini-only gates.** A single per-(kind, provider family) predicate replaces `carriesPDFParts`/`carriesVideoParts`: PDF parts ride for `gemini`, `anthropic`, and `anthropic-compatible`; video stays gemini-only but moves onto the same matrix; images keep no connector gate (every family carries image parts). The stored model row remains the per-model gate; for `anthropic-compatible` endpoints the checkbox is the user's assertion that the endpoint passes document blocks. Rationale: each cell is verified in connector source, and a matrix constant is flippable when a live smoke disagrees (see Risks). Alternative rejected: runtime capability probing per endpoint — no cheap, reliable probe exists, and the checkbox already encodes user intent.

**D2 — Mirror at turn assembly, copy-if-absent.** The turn composer gains the agent workspace directory and mirrors every attachment riding the turn into `workspaces/<slug>/uploads/` before the model input is built, so the note can address the final (possibly collision-suffixed) name. Copy-if-absent keeps regenerations and replays idempotent — replayed user messages reference the same files. Naming: sanitized original filename, numeric suffix on collision. Mirror failure is non-fatal: the note falls back to the capability key alone. Alternatives rejected: placement at upload time (`SaveAttachment` has no session/agent context and would orphan cancelled uploads), and mirroring only when degrading (non-uniform; a capable model loses the option to `read_file` an earlier upload in a later turn).

**D3 — Pointer note contract.** Degraded attachments produce one stable note shape: `[attached file not sent: <name> (<mime>, <size>) — <reason>; read it at /workspace/uploads/<suffixed name> (fallback: attachment key <key>)]`. The reason names the true gap: `the model cannot read PDF input` vs `the provider connector cannot carry PDF parts`. The office-document lane switches to the same uploads handoff (`read_file` presents its structured view), dropping the save-into-knowledge instruction. The exact strings live beside the note builder as test fixtures.

**D4 — Tool-result carriage flag and the text fallback.** The same matrix derives a per-turn `carries file parts in tool results` flag handed to the jailed filesystem backend at compose time. When false, `read_file` on a PDF returns extracted text (result states the bytes were withheld); a document with no extractable text returns a clear error result. Extraction sits behind a narrow function seam; the first implementation uses the pure-Go extractor already in the dependency tree via document-editing (pdfcpu), swappable without touching the design. Family cells: OpenAI family and gemini extract (both verified to error on tool-result file parts); the anthropic family starts as *inline* — its connector maps tool-result files to document blocks — with a live-smoke task that flips the cell to *extract* if the API rejects a document inside `tool_result` content.

**D5 — `save_document` stays opt-in curation.** No behavior change; only its model-facing description sheds the degradation framing. `attachment_key` keeps reading the canonical store, so saves work regardless of mirror state. The knowledge corpus returns to its designed role: documents deliberately promoted, never a landing zone.

**D6 — Composer warning from the same matrix.** The capabilities binding payload grows a carriage field (e.g. `pdf_parts`) next to the existing `pdf_input`, computed from D1's matrix for the session's provider family. The composer chip warns "PDF will arrive as a reference note (this provider cannot carry PDF parts)" when `pdf_input && !pdf_parts`, keeping the existing warning for `!pdf_input`. Wails bindings regenerate.

**D7 — Path exposure, deliberately scoped.** The storage requirement's "no raw path" line is reworded, not deleted: the frontend keeps capability URLs as its only attachment channel, while the model — which already owns all of `/workspace` — may be told `/workspace/uploads/<name>` in a reference note. No new trust boundary: the mirror is inside the jail's existing read-write root.

## Risks / Trade-offs

- [Z.AI's anthropic-compatible endpoint may strip or reject PDF document blocks] → D1 and D4 are matrix constants; the Z.AI live smoke verifies both the direct part and a tool-result read, and a failure flips the family to degrade/extract — no turn fails either way.
- [pdfcpu text extraction may be lossy (scans, complex layout)] → the seam isolates the implementation; the no-extractable-text case returns a readable error result rather than garbage.
- [Uploads accumulate in workspaces] → bounded by existing caps (4 files/message, 20 MB max); the workspace is user-managed space; retention is explicitly out of scope.
- [Matrix constants can drift from connector upgrades] → the matrix module cites the verifying connector versions; the existing connector-upgrade test pass gains an assertion per cell.
- [Mirrored copies double disk for carried attachments] → accepted: canonical store remains authoritative for the frontend and capability URLs; local disk, bounded sizes.

## Migration Plan

None required. The mirror is lazy and copy-if-absent, so previously stored attachments land in `uploads/` on their next carrying turn. Rollback is a plain revert; leftover `uploads/` directories are inert workspace files the agent already had permission to read.

## Open Questions

None blocking. The two live smokes (Z.AI direct part + tool-result read; OpenAI-family extraction path) settle matrix-cell constants, not the approach.
