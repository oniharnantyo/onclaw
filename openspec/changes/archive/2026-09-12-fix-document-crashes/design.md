## Context

The eino `libs/acl/openai` ChatCompletions converter (`agentic_convert.go`) errors on `schema.UserInputFile` blocks — verified against the pinned module (`v0.1.18-0.20260527084435-846f52bd97c6`, the `default:` arm). The `agenticclaude` and `agenticgemini` connectors both convert file blocks natively. `internal/agents/model_factory.go` routes three provider types (openai, openrouter, openai-compatible) through the broken connector.

Two seams deliver file blocks to that converter:

1. **Live turn** — `buildAttachmentUserMessage` (attachments_message.go) emits a byte-carrying block when `mod.pdf != InputUnsupported`. The catalog axis marks many models "unknown" (no models.dev evidence), and the fail-open rule sends the block anyway — converting "we don't know" into a guaranteed crash on the OpenAI family.
2. **Manual compaction** — `loadSessionWindow` replays the persisted log, whose attachment blocks are reference-form (bytes demoted at persist). The normal turn path protects against these via the model-time stale-block expansion (current-turn-only policy), but `mw.Summarize` calls the model directly on the raw window — bypassing that middleware — so reference-form file blocks (and stale URL-only image blocks, which the OpenAI family also cannot resolve) reach the connector unconverted.

Normal-turn history replay is already safe: persisted blocks are reference-form and the existing model-time expansion placeholder-collapses them.

## Goals / Non-Goals

- Goals: OpenAI-family agents can carry PDF attachments (degraded) and can compact any session containing attachments.
- Non-Goals: changing upload lanes or caps; extracting PDF text server-side (that is `add-document-read-tool`'s drop-lane routing); fixing the upstream eino converter; altering the claude/gemini live path where their connectors accept file blocks; touching the current-turn-only policy.

## Decisions

- **D1 — One predicate: "connector accepts file blocks".** A single seam (`model_factory.go` already owns connector routing) exposes whether the resolved provider's connector converts `schema.UserInputFile`. True: anthropic, anthropic-compatible, gemini. False: openai, openrouter, openai-compatible.
  - *Alternative considered:* deriving it from the model catalog's `pdf` modality — rejected: the catalog answers "can the model view PDFs", but the crash is connector-level and affects models with `pdf: true` too.
- **D2 — Live gate degrades, does not error.** On the live path, a PDF ref whose connector cannot accept file blocks takes the existing `degradedAttachmentNote` (D4 pointer note) regardless of the catalog axis — the connector gate ANDs with the catalog gate, and the connector gate wins on "unknown" for file blocks. Images keep today's tri-state fail-open: the OpenAI ACL converter accepts image blocks, so unknown is genuinely safe there.
  - *Alternative considered:* routing PDFs to the drop lane now — rejected as scope creep; that is `add-document-read-tool` with its own upload/lane/UX work.
- **D3 — Compaction reuses the exact stale-block expansion, not a file-only variant.** The expansion logic (`isStaleAttachmentBlock` + `attachmentPlaceholderText`) is extracted into a shared helper the turn middleware and `loadSessionWindow` (or the compaction call site) both apply. Stale *image* blocks in the window are expanded too: the OpenAI connector accepts image blocks in principle, but a URL-only image block gives the provider an unfetchable server-relative URL — a different, equally fatal failure the middleware normally prevents.
  - *Alternative considered:* teaching the eino summarization middleware to run rewrites — rejected: upstream surface, and the window is fully ours at the call site.
  - *Alternative considered:* sanitizing only file blocks — rejected: it would trade the known crash for a provider-side fetch failure on stale images.
- **D4 — Placeholder semantics stay as-is.** The window's expanded placeholders are the existing model-facing text (`[attachment: name (mime) — attached in an earlier message …]`). The summarizer loses no information it would otherwise have — today it gets a crash.
- **D5 — Persisted events are never rewritten.** The transform is conversion-time only. Persisted blocks remain the provider-neutral transcript fidelity source; transcript projection reads block metas and is unaffected.

## Risks / Trade-offs

- [Compaction summaries reference attachments only by name/path, not content] → Strictly better than today (crash). When `add-document-read-tool` lands, summaries of drop-lane documents gain the read instruction.
- [Predicate drift if eino fixes the ACL converter] → The gate is one seam; when a future eino version converts `UserInputFile` on the OpenAI family, flip it and the live path reopens.
- [The gate makes "pdf: true" catalog models on OpenAI family degrade silently] → The pointer note text explains the limitation to the model; the transcript pill keeps the file visible to the user. The `add-document-read-tool` design updates the note to name `document.read`.

## Migration Plan

No schema or data migration. Deploy is additive; rollback is a revert. Broken sessions recover on the next action: turns already worked via the existing expansion (for sessions whose PDF turn is fully persisted), compaction works after deploy.

## Open Questions

None — the `add-document-read-tool` change owns the next evolution (uniform drop-lane routing retires the live gate here; the summarize-window expansion stays durable).
