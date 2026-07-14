## Context

**Current State:**
onclaw compacts long conversations via Eino's `summarization` middleware, configured in
`internal/agent/agent.go` with only `Model`, `Trigger.ContextTokens = 0.8 * window`, and a
`Callback` that persists the summary. `SaveSummary` inserts the summary as a turn row and
records `summary_message_id` + `summary_until_seq` on the `conversations` row; `LoadHistory`
returns `summary + last 3 turns` for the agent, while `ListTurns` returns every row
(including the summary) for the UI. The summary row's token columns are unset, and nothing
distinguishes it from a real assistant turn.

The UI (`ChatProvider.fetchMessages`) renders every turn through one path; the context meter
reads the last turn's `prompt_tokens`. Eino's default token counter anchors on the last
assistant message's `TotalTokens` and estimates newer messages at chars/4. `Retry` is unset,
which defaults to no retries.

**Problem:**
- The summary row renders as a normal assistant message (a "mystery recap"): nothing
  distinguishes it and the chat-ui "flat content-forward" rule gives it no marker.
- The meter drops on the compaction turn (the post-compaction `prompt_tokens` is small) with
  no signal that compaction occurred.
- Summary generation has zero retries, so a transient failure during compaction can fail the
  turn.
- The trigger anchors on `TotalTokens` (prompt + completion); for a long-output coding agent
  the last completion inflates the measurement, firing compaction early.
- The agent has no path back to exact compacted detail; the summary abbreviates it and
  `session_search` FTS is the only recall route.
- Summarization is applied blindly: it fires on total tokens but can only compress history,
  so when the fixed floor (system prompt + tool schemas) already exceeds the budget it churns
  helplessly and masks the overflow (the 5000/7400 case).

**Constraints:**
- Stay provider-agnostic (no Anthropic-only server-side compaction).
- Keep append-only retention; do not delete or hide non-summary messages.
- No client-side tokenizer (low-resource target); keep the anchor + chars/4 shape.
- Agent-side bounded replay is already correct and must not change.

## Goals / Non-Goals

**Goals:**
- Make compaction visible and explained on both sides: a transcript marker for the user, a
  re-readable transcript for the agent.
- Make the trigger measure input fill and fire resiliently.
- Do it without changing bounded replay or retention.

**Non-Goals:**
- Hide or collapse old (non-summary) messages.
- Replace Eino's summarization with provider server-side compaction.
- Introduce a tokenizer or client-side token caching.

## Decisions

### Decision 1: Flag summary rows with an `is_summary` column (not derive from the cursor)

**Rationale:**
- The active summary is identified by `conversations.summary_message_id`, but after a
  re-compaction there are multiple summary rows and only a per-row flag identifies them all.
- The coverage cursor `summary_until_seq` is non-derivable — Eino summarizes a prefix and
  keeps recent turns, so the cursor is not `summary_row_seq - 1`; it stays as the recorded
  highest-discarded `sequence_num`. The flag is purely for rendering/metadata.
- A per-row flag set at insert is append-only-safe and one migration; the summary remains a
  first-class timeline citizen (it needs a `sequence_num` for marker placement and flows
  through the existing unmarshal/render pipeline).

**Alternatives Considered:**
- *Expose only `summary_message_id`.* Rejected: superseded summary rows after re-compaction
  would render as bubbles again (the bug returns).
- *A separate `summaries` table.* Rejected: breaks the summary's place in the ordered turn
  stream that the marker relies on.

### Decision 2: Marker = divider + collapsible summary; old messages stay visible

**Rationale:**
- The operator wants old messages to remain visible, so this is "marker on top of full
  retention", not hide-and-collapse.
- The summary is injected context, not an utterance; rendering it as a marker (not a flat
  assistant bubble) fixes the semantic error and the "mystery recap".
- Reusing the existing collapsible idiom (`Reasoning` / `ToolGroup` / `ChainOfThought`) keeps
  it cheap and design-system-consistent.

### Decision 3: Trigger anchors on `PromptTokens`, adds `ContextMessages` backstop and `Retry`

**Rationale:**
- The context-window limit is on input tokens; `TotalTokens` over-counts by the last
  completion. `PromptTokens` measures real input fill and is provider-agnostic (present in
  `TokenUsage` for every adapter).
- `ContextMessages` is a cheap backstop for pathological message counts ("triggers if ANY
  condition is met").
- `Retry` (default off today) makes compaction best-effort; bound `MaxRetries` to limit
  latency on the compaction turn.

**Alternatives Considered:**
- *Real tokenizer (tiktoken) for the increment.* Rejected: conflicts with provider-agnostic,
  no-client-side-tokenizer, low-resource design; the anchor already covers the bulk.
- *Anthropic `/count_tokens` endpoint.* Rejected: network call in the hot loop,
  provider-specific; the anchor reuses already-reported usage for free.

### Decision 4: `TranscriptFilePath` via transcript-file export

**Rationale:**
- Eino appends the path to the summary ("read the full transcript at <path>"), giving the
  agent a way back to exact compacted detail — the agent-side complement to the user-side
  marker.
- onclaw already retains the compacted range in SQLite; exporting it to a per-conversation
  text file under the workspace/config dir is the simplest concrete target Eino can `Read`.

**Alternatives Considered:**
- *Virtual path backed by the DB (no file).* Viable and avoids file writes, but needs fs
  backend plumbing; noted as a future option.
- *Rely only on `session_search` FTS.* Works for recall but the summary prompt has no path to
  cite; `TranscriptFilePath` is the direct pointer Eino's prompt expects.

### Decision 5: Floor safety guard, tripping below the summarization trigger

**Rationale:**
- Summarization's hidden precondition is "the fixed floor fits." When violated it is a
  no-op-with-cost — it runs, cannot help, and masks the real failure. The guard makes that
  precondition explicit and fail-fast.
- The trip point is `floorSafetyFraction × context_window` (default 0.5), deliberately *below*
  the 0.8 summarization trigger: the floor must be gated before summarization's effectiveness
  is compromised, not at the same line. At 0.5 only genuinely bloated floors trip (default
  64000 window ⇒ 32000 ceiling; a normal ~13k floor passes; the 7400/5000 case fails).
- Measured against the resolved configured window (`max_context_tokens`), the value the
  operator sets and expects enforced.
- The floor is estimated with the existing chars/4 estimator over the instruction string plus
  each tool's marshaled schema (mirroring the summarization `TokenCounter`) — provider-agnostic,
  no tokenizer.
- Two enforcement points: a build-time guard in `AssembleAgent` (refuses to start; estimates
  from the finalized tool set), and a per-turn preflight middleware ordered *before*
  summarization that re-estimates the tool floor from the live `state.ToolInfos` each turn (the
  authoritative runtime gate; on failure it fail-skips summarization).
- A sentinel error lets the API map it to HTTP 400 (a config error), not 500.

**Alternatives Considered:**
- *Trip at the 0.8 summarization line.* Rejected: that is summarization's own line; the floor
  would already have consumed the budget summarization needs.
- *Trip only at 1.0 (true overflow).* Rejected: catches only physically-unsendable inputs,
  missing the degraded-but-fits zone where summarization churns uselessly.
- *Measure against the model's real window (metadata).* Rejected for v1: `max_context_tokens`
  is the operator's stated budget; the metadata window is a future refinement.

## Risks / Trade-offs

**Risk:** `is_summary` migration on existing rows.
- **Mitigation:** Backfill via `summary_message_id`; pre-release, default-0 is acceptable.

**Risk:** Transcript files accumulate on a low-resource device.
- **Mitigation:** One small text file per conversation, written once per compaction; bounded.

**Risk:** `Retry` adds latency to the compaction turn.
- **Mitigation:** Bound `MaxRetries` (e.g. 2); a delayed turn is better than a failed one.

**Trade-off:** The meter still drops after compaction (correct — it reflects the real
post-compaction fill); the annotation explains it rather than redefining the metric.

**Risk:** The build-time floor estimate undercounts tools injected by middleware at request
time (the filesystem-middleware tools), since those are not materialized in the finalized tool
set.
- **Mitigation:** The per-turn preflight re-estimates from the live `state.ToolInfos`, so it is
  the authoritative gate; the build guard is the fast early check. The guard targets gross
  misconfiguration (large persona on a small window), which both catch robustly.

**Risk:** `floorSafetyFraction = 0.5` may refuse some large-but-legitimate agents.
- **Mitigation:** It is a named, tunable constant; the actionable error names floor / limit /
  window so the operator can raise `max_context_tokens` or trim the persona with full context.

## Open Questions

- Transcript target: file export (default) vs. virtual DB-backed path — revisitable.
- `ContextMessages` threshold value (default 200) — tunable.
- `floorSafetyFraction` value (default 0.5) — tunable.