## Why

A Langfuse trace (`5a720718-…`) of a `master` agent session ended in
`[NodeRunError] failed to convert agentic messages: unsupported content block
type "assistant_gen_text" in user message` — the run crashed immediately after
its first summarization compaction, leaving the chat UI stuck "loading" with no
turn rendered. Root-causing that crash surfaced three more defects in the same
subsystem, all sharing one shape: the compaction callback performs cross-layer
side-effects over an undocumented summary-message contract.

The compaction subsystem has **drifted from its spec**:

- **A spec violation.** `agent-memory` already requires a disabled extraction to
  perform *no* archival (see `Disabling extraction stops archival` /
  `Memory extraction flushes before compaction`), yet the compaction callback
  calls `ExtractAndFlush` directly, ignoring the `ExtractionEnabled` flag — so
  extraction runs with all memory disabled and then 400s an embedder configured
  with an empty model name (`error.json`: `Config.Model: ""`).
- **Spec gaps.** `conversation-history` never defines the summary message's
  role/block-type validity (the crash); defines `answer` as "last assistant block
  text" with no rule for summary rows that carry no assistant block; and
  specifies only post-hoc `compaction_count` — no real-time compaction or
  usage signal to the UI.

## What Changes

**Correctness fixes (close the violation + the gaps):**

- **Cluster 1 — post-compaction crash.** Stop appending the recency note as an
  `assistant_gen_text` block onto the `role: user` summary message; use
  `user_input_text` so the message stays role-valid for provider conversion.
- **Cluster 2 — extraction-when-disabled + embedder 400.** Gate the
  compaction-time `ExtractAndFlush` on `ExtractionEnabled` (matching the
  turn-end path), and guard `Embedder.Embed` against an empty model name.
- **Cluster 4 — summary `answer`.** `SaveSummary` extracts `answer` from
  `user_input_text` blocks too (where Eino puts the summary), not only
  `assistant_gen_text`, so `answer` + the FTS index carry the real summary.
- **Capstone — `SummaryMessage` contract.** Define and enforce the summary
  message invariant (single `role: user`, only `user_input_text` blocks):
  validate before persistence (reject) and sanitize on replay (strip
  role-invalid blocks, so legacy bad rows degrade gracefully instead of
  crashing — no backfill migration needed).

**UX feature (close the real-time gap):**

- **Cluster 3 — progressive context meter + live compaction indicator.** An
  `EventSink` lets middleware callbacks emit mid-run `usage` and `compaction`
  SSE events; the meter ticks per model step and a "Compacting context…"
  indicator shows during summarization.

**Follow-up redesigns (documented in `design.md`, not in this change's task
list):** move the recency note out of the durable summary into a fresh
per-turn injection (dissolves Cluster 4 + fixes staleness); and route
compaction-time extraction through the memory layer (`memMW.ExtractCompacted`,
owns its own flag) instead of the callback calling `ExtractAndFlush` directly.

## Capabilities

### Modified Capabilities

- `conversation-history`: add the summary-message contract requirement
  (role/block-type validity, validate-before-persist, sanitize-on-replay) and
  the summary-row `answer` denormalization rule.
- `agent-memory`: add that compaction-time extraction is gated by the extraction
  flag at *every* call site, and that embedding degrades to FTS-only when no
  embedding model is configured.
- `chat-ui`: add real-time context-usage updates and a live compaction indicator.

## Impact

**Affected code:**

- `internal/agent/agent.go` (`handleSummarization`: recency block type,
  extraction gate, capstone validate, EventSink wiring)
- `internal/agent/event.go` (new — `EventSink`)
- `internal/agent/middlewares/history_middleware.go` (replay sanitize, usage emit)
- `internal/api/handler/chat.go` (EventSink → SSEWriter)
- `internal/memory/embedding.go` (empty-model guard)
- `internal/store/sqlite/conversation.go` (`SaveSummary` extraction + test)
- `web/src/types/chat.ts`, `web/src/components/chat/runChatStream.ts`,
  `web/src/components/ChatProvider.tsx`, `web/src/components/Chat.tsx`

**Affected systems:** compaction/summarization, conversation replay + provider
conversion, memory extraction + embedding, FTS searchability of summaries, the
chat SSE stream + context meter UI.

**Dependencies:** none new. Reuses the existing `conversation_messages_ai` FTS
trigger and the `RecencyTracker` KV spill.

**Non-goals:** no backfill of existing rows (replay sanitization handles legacy
data); the recency + extraction layering redesigns are documented follow-ups,
not in this change's task list; the heavy `SummaryMessage` wrapper type is
deferred (the summary is single-block after the recency patch, so there is little
to wrap).