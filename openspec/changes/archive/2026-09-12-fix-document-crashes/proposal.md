## Why

Sessions containing inline PDF attachments fail unrecoverably on OpenAI-family providers (openai, openrouter, openai-compatible): the eino `libs/acl/openai` ChatCompletions converter has no case for `schema.UserInputFile`, so any model call that receives the block fails with `unsupported content block type "user_input_file"`. Two seams are broken today: the live turn that carries the attachment, and manual compaction — `summarize: failed to generate summary` — because the summarizer bypasses the model-time attachment expansion that otherwise protects normal turns. Both were hit in production.

## What Changes

- **Connector-aware live gate**: OpenAI-family models never receive a byte-carrying `UserInputFile` block on the live-turn path; inline PDFs degrade to the existing marked pointer note (D4 machinery, connector axis added alongside the catalog axis). The connector gate wins on unknown capability — fail-open no longer sends a block the connector cannot convert.
- **Summarize-window expansion**: the compaction path (`compact.go`) applies the existing stale-attachment expansion — the same `isStaleAttachmentBlock`/placeholder logic the turn middleware uses — to the loaded message window before `mw.Summarize`. Today that window bypasses the middleware, so persisted reference-form file blocks (and stale URL-only image blocks) reach the connector raw. After this change, compaction succeeds on any session containing attachments.
- Normal-turn history replay is NOT touched: the existing current-turn-only expansion already collapses persisted reference-form blocks into placeholders on the turn path.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the "Model-modality attachment degradation" requirement gains the connector axis — a connector that cannot accept file blocks forces the degraded pointer note regardless of catalog capability (unknown no longer fails open for file blocks). The "Context summarization" and "Manual compaction command" requirements gain the window-expansion behavior: the summarizer never receives raw attachment blocks.

## Impact

- `internal/agents/attachments_message.go` (live gate), `internal/agents/runner.go` (modality resolution gains the connector axis), `internal/agents/compact.go` (window expansion before Summarize), and the expansion helper extracted for reuse by the middleware and compaction.
- No wire/API changes; no DB migrations; no frontend changes (transcript pills already project from stamped block metas).
- Follows `add-document-read-tool` (separate change) where uniform drop-lane routing retires the live gate; the summarize-window expansion remains durable.
