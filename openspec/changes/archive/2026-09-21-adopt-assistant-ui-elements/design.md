# Design: adopt-assistant-ui-elements

## Context

OnClaw's chat already renders ~12 assistant-ui gallery patterns in its own design language. The adopted set targets the gaps (see proposal). Ground-truth constraints this design works within:

- **The tool loop is server-side eino.** assistant-ui's rich elements run on *frontend tools* (the model calls tools that execute in the browser). OnClaw has no such mechanism — so structured output reaches the UI via server-side tools whose envelopes the transcript renders (D3).
- **The wire delivers tool args/results complete** (`output_item.added` argless → `output_item.done` full). There is no mid-stream JSON parse, so every "lands one by one" effect is a client-side stagger reveal after arrival — the gallery's standalone-mode parity, never a claim about live parsing.
- **Usage ground truth:** the runner accumulates per-call usage; `FinalInputTokens` is the last call's input (the model's context pressure). Providers report totals only — no per-category split exists at any tier; every split is the same ~4 chars/token estimator, differing only in *coverage* (client transcript vs. server composition vs. per-call request).
- **Composition is invisible to the client:** persona docs, workspace/user docs, `## Shared memory`, per-turn memory, retrieval prefetch, channel docs, profiles, real eino tool schemas. A client-side estimate is blind to all of it — the segment this product most wants to see (memory) is the one the client cannot count.
- **Existing schema conventions:** session-scoped tables reference sessions by the text `session_id` (logical, against `agent_sessions.session_id`'s UNIQUE) rather than a hard FK — `session_events` precedent; `agent_todos` follows it.

## Goals / Non-Goals

Goals: one full-stack change (FE+BE paired per workstream, per user directive); honest accounting of what is measured vs. estimated; no breaking wire changes; cards never muted (collapse is additive); plugin-extensible structured-output surface.

Non-Goals: the gallery's conversation map (undefined element — deferred pending a user pick among Flow graph / Search-in-conversation / skip); per-call request measurement (T2 in the model wrapper — future upgrade behind the unchanged `context_breakdown` contract); voice/orb/canvas/computer-use/surface elements; cost meter and quota UI (no pricing/quota data on the wire); streaming-tool-args partial parsing.

## Decisions

- **D1 — Ring: pure assistant-ui, in the composer rail.** SVG donut in the composer's left control rail (popover opens upward); fixed severity ladder accent <65% / amber 65–85% / danger >85%; the summarization-trigger tick is dropped *by design* — the transcript's compaction divider is the only "it happened" signal. User-locked (both overrides: placement reversed the earlier header lock; tick dropped against my "both signals" rec). Visibility rules unchanged: agent chats with usage and a known window only; header loses the meter entirely. Alternatives considered: trigger-anchored amber (fails for agents without summarization configured), header placement (reversed by user).

- **D2 — Breakdown: honest remainder, not scale-to-fit.** Segments (Instructions / Tools & skills / Files / Conversation) at face value, never scaled; **Server context** row = used − sum(estimates), floored at 0, with a provenance tooltip; Headroom derived legend-only (assistant-ui rule); estimates > used → floor + clamp with the caption still reading "estimated". Conversation counts only entries after the latest compaction divider (pre-compaction bulk is server-summarized; the summary lands in Server context naturally). User-locked over scale-to-fit, which launders memory into every row. Estimate constants (~4 chars/token; 8 tok/message framing; 120 tok/tool; 40 tok/skill; flat 1100 tok/image) live in one pure lib (`contextBreakdown.ts`) — the parked scale-to-fit implementation is rewritten to these semantics.

- **D3 — Echo tools are OnClaw's frontend tools.** `ui.chart` / `ui.timeline` / `ui.preview` are real Go tools that validate their schema and echo their args as the result; the transcript renders the envelope through the registry. No new wire events, no persistence, rides the existing tool-call path (SSE live, session_events durable, History re-render). Alternative rejected: markdown fenced-block conventions (` ```chart `) — unparseable mid-argument, no call identity, not registry/plugin-shaped. Preview sandboxing: `sandbox` **without** `allow-same-origin` (assistant-ui's per-render-origin isolation is not portable); height fixed by the card; reload remounts by key.

- **D4 — Registry first.** A `$type`-keyed spec renderer (recognized `$type` → renderer, remaining keys → props, `children` → nested specs; unknown `$type` → nothing user-visible; guard test pins entries) built before any element lands. Web search card is the first *real-tool* entry (envelope sniffed from `web.search` results — `{title,url,snippet}` already on the wire); chart/timeline/preview/todo cards are echo-tool entries. Plugin story per AGENTS.md: plugins register renderers like any other registry surface.

- **D5 — Todos are table-backed (user rejected transcript-as-store).** Migration `000061_agent_todos`: workspace/agent FKs CASCADE, `session_id` text (logical ref, house convention), `item_key`, `item_text`, `status` (pending|active|done|failed), `reason`, `revision`, `UNIQUE (session_id, item_key)`, `INDEX (workspace_id, agent_id, status)`. `todo_write` = real write tool: validate → one transaction (upsert by key, delete vanished keys, bump revision) → echo result. Transcript keeps revision history (cards render from event args); the table is current state — open items are an indexed query, never a payload scan. Rationale that killed the echo design: compaction drops the verbatim plan from the model's window; "current state" would require LIKE-scans; task state deserves task-state lifetime and integrity. Rewrites use stable item keys so rows restyle rather than remount.

- **D6 — Todo re-grounding: inject summary, full list behind `todo_read`.** System context carries a one-line open-items summary ("4 open, 1 active") each turn; `todo_read` returns the full list. Zero per-turn cost beyond one line; the model spends a call only when the plan matters. Coordinator default (carried, user not asked directly) — alternatives: separate tool only (free until asked, but models under-reach), full injection (token cost every turn).

- **D7 — Context measurement lives in this change.** Compose-time section byte counts (the instruction composer already assembles the sections in order) + `estimateWindowTokens` over the true session window at turn end — the same estimator the compaction divider uses. Emitted as `usage.context_breakdown` (omitempty; additive; display-only — never billing/trigger/summarization inputs). The client estimate stays the fallback for old hydrated sessions. Coordinator default carried (follows from the user's FE+BE-together directive). T2 (per-call measurement in the model wrapper, matching `FinalInputTokens` semantics exactly) stays a future change.

- **D8 — Collapse at ≥4 calls; never mute.** Turns with ≥4 tool calls render a "N steps · M files changed" timeline header (churn derived from `edit_file` old/new_string — the diff parser exists); expanding reveals the existing inline cards in stream order. Light turns unchanged. Assistant-ui's "mute per-tool renderers" guidance is explicitly rejected — OnClaw's tool cards are a design-contract feature.

- **D9 — Queue replaces the same-tab 409 path only.** Send-while-running queues locally (store state, cancelable rows, auto-dispatch first-in-order on completion). Cross-tab/cron conflicts keep the existing catch-up-and-redispatch machinery. Rationale: the client knows `running` before sending; the server lock remains the source of truth for foreign conflicts.

- **D10 — Math via remark-math + rehype-katex in `MarkdownBody`** (~300KB gz, the change's only new dependency). Models emit `$…$` naturally; no tool needed. Coordinator default carried.

- **D11 — Small transcript elements:** message timing (client-measured first-token/total/tok-s on hover, live turns only, present-only), day separators (Today/Yesterday/date at day boundaries; hydrated history already carries RFC3339 `occurred_at`; live entries gain an ISO `at` alongside the display `ts`), draft restore (localStorage per thread, cleared on send, never synced), number ticker (rAF count-up on the popover's used figure).

## Risks / Trade-offs

- [Server context row shows a large opaque number early (estimate + remainder framing)] → provenance tooltip names exactly what it holds; D7 replaces the remainder with measured segments in the same change.
- [Estimator skew (~4 chars/token vs provider BPE) makes segment sums drift from used] → total is always the provider's real number; segments are labeled estimated; skew is bounded and identical to the compaction divider's existing display contract.
- [Echo tools spend a model call per structured element] → accepted: they carry schema validation, call identity, and durability that markdown conventions cannot; descriptions keep models from over-calling.
- [Model misreports todo statuses] → same trust model as reasoning text; the table constrains shape, not truth; revision history in the transcript makes drift visible.
- [KaTeX bundle weight] → ~300KB gz, lazy-loaded only when a message contains math delimiters.
- [Queue auto-dispatch races a session that went busy elsewhere] → dispatch goes through the normal send path, so a 409 falls back to the existing catch-up flow.
- [Parked `contextBreakdown.ts` implements the rejected scale-to-fit] → rewritten at implementation start; the store/runtime wiring around it is kept.

## Migration Plan

Single migration `000061_agent_todos` (additive table, no backfill needed — pre-change turns simply have no todo rows). Wire addition is omitempty and additive; old clients ignore it. Frontend degrades per element (ring hidden without usage; breakdown falls back to estimates when the wire lacks the split; cards render only for recognized envelopes). Rollback: drop the table, revert the frontend — no data shape other services depend on.

## Open Questions

- The deferred conversation-map pick (Flow graph / Search in conversation / skip) — resolved by the user before or after this change; neither candidate is in scope.
- T2 per-call measurement timing — sequenced after the wave-3 memory work lands; the contract does not change.
