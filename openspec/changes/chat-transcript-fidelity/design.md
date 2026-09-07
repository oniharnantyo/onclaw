## Context

The chat pipeline has fidelity drops at four seams (see proposal.md — Why). Current state, verified on disk:

- The domain event schema is reasoning-ready (`agents.TranscriptEventReasoningDelta`, `CompletedMessage.ReasoningContent`) and the translator already maps it to `onclaw:reasoning_delta` — but the runner's drain loop never extracts reasoning blocks, so nothing upstream of the wire ever fires.
- Eino delivers reasoning as `ContentBlockTypeReasoning` content blocks on AgenticMessage frames (with stream-frame consolidation); `extractAgenticText` reads only `AssistantGenText`/`UserInputText`.
- Tool call arguments flow to the wire (`output_item.added` carries `arguments`), but the web `onToolCall(name, callId)` callback has no args parameter. Tool results are emitted as `onclaw.function_call_output` **items** inside `response.output_item.added`/`.done` (pinned by `translate_test.go`), but the web client switches on a top-level `onclaw.function_call_output` event type that never occurs — so `onToolOutput` never fires live.
- Nobody sets `ToolResultPayload.Latency`; every card shows `0ms`.
- `Runner.History` projects tool spans without joining to the persisted assistant/tool-result messages (eino's `ToolSpanMeta.AssistantMessageEventID` / `ToolResultMessageEventID` exist precisely for this join) — hydrated cards get no args/results/latency/reasoning.
- `ToolCall.tsx` fabricates `'ok — Nms, M rows'` when `t.res` is empty, which is every live card.
- The workspace tools catalog already exposes `display_name` (`List Files`, `Shell`, …) and the web already fetches it for settings (`api.tools.list`).
- `session_events.payload` is `bytea`; `HumanReadableSerializer` emits JSON, so it displays in psql as hex gibberish.

## Goals / Non-Goals

**Goals:**
- One turn renders in the transcript exactly what happened: reasoning, tool calls with real args, real results, real latency, real errors — live and hydrated.
- Kill every fabricated content path in the transcript UI.
- Make session payloads readable in SQL.

**Non-Goals:**
- Durable (cross-reload) error markers: persisting failures into the session event log risks polluting the model-replay context (the log feeds full-session replay). Error entries are local to the loaded thread; durability is deferred until a replay-safe seam exists (e.g. run-status lookup on hydration).
- Syntax highlighting libraries for code blocks; GFM extensions beyond the basics.
- Server-side JSON querying of payloads (that would argue for jsonb; `text` is chosen for debuggability — ad-hoc queries cast with `::jsonb`).
- Changing the seeded demo thread content semantics (seeded cards gain result text only so they stay complete once the fabricated fallback dies).
- The assistant-ui bridge surface (`convertMessage` is vestigial for rendering); removing it is a separate cleanup.

## Decisions

**D1 — Tool trace wire shape: keep the implementation, amend the spec.** The trace stays `onclaw.function_call_output` items delivered via standard `response.output_item.added`/`.done` (dot, item-shaped), not the spec's previous `onclaw:function_call_output` (colon) wording. Rationale: the item shape rides the standard output-item lifecycle and lands in aggregated `response.output` for non-streaming clients; a colon-named top-level event would not. It is also what `translate_test.go` pins. The naming asymmetry (`onclaw:reasoning_delta` colon event vs `onclaw.function_call_output` dot item) is deliberate: colon events are stream notifications with no item semantics; dot items are response output.

**D2 — Reasoning extraction in the runner.** In `drainAgentEvents`, extend frame handling: extract `ContentBlockTypeReasoning` text from each frame and emit `TranscriptEventReasoningDelta` per consolidated chunk (eino consolidates reasoning across stream frames — see schema consolidation tests). Non-streaming assistant messages emit reasoning once with the completed message. Reasoning is never persisted as deltas (per the existing "deltas SHALL NOT be persisted" requirement); durability comes from the full AgenticMessage the ADK machine already serializes, which `History` projects (D4). `RunTurn`-level ordering is already correct: the translator emits `onclaw:reasoning_delta` in event order.

**D3 — Tool latency: runner-side stopwatch.** `emitToolStarted` records `time.Now()` per call id (the existing `startedTools` map gains a companion `startedAt`); `emitToolFinished` stamps `ToolResultPayload.Latency` from the difference. This covers both the message-driven and span-driven emit paths without touching persistence. The translator already forwards `Latency.Milliseconds()` as `latency_ms` when > 0. In `History`, latency comes from the persisted span pair's timestamps (start span → end span joined by `ToolUseID`/`ToolCallStartEventID`).

**D4 — History joins for fidelity.** `History` already loads all rows and deserializes each; add a pre-pass building two maps from persisted events: assistant-message event id → message (its `FunctionToolCall` blocks carry call id + arguments), and tool-result message event id → result content. When projecting `SessionEventSpanToolCallStart`/`End`, resolve args via `ToolSpanMeta.AssistantMessageEventID` and results via `ToolResultMessageEventID`, falling back to call-id matching across the maps. Assistant completed messages gain `ReasoningContent` extracted from their persisted reasoning blocks. Alternatives considered: SQL-side extraction (rejected — needs jsonb and moves Eino-type awareness into the store layer, breaking the opaque-payload seam), and streaming-side re-assembly only (rejected — hydration must match live).

**D5 — Web client dispatch.** `openresponses.ts` changes: (a) new `onReasoningDelta` callback wired to `onclaw:reasoning_delta`; (b) `onToolCall(name, callId, args)` gains the arguments from `ev.item.arguments`; (c) tool outputs routed by inspecting `ev.item?.type === 'onclaw.function_call_output'` inside `response.output_item.added`/`.done`, replacing the dead top-level case. The openai SDK yields unknown event/item types verbatim at runtime (verified from SDK source), so no dependency change is needed.

**D6 — Markdown rendering.** `react-markdown` (no GFM plugin, no highlighter; code blocks styled JetBrains Mono per the design contract, raw HTML stays escaped by default). `AgentMessage` renders the text through a memoized markdown component; `MentionText` mention highlighting moves into a text-node renderer override so `@Handle` chips survive inside markdown output. Streaming re-parses per delta — accepted for chat-length messages (components memoize on text; revisit only if profiling says otherwise). Alternatives: `marked` + sanitiser (more control, more responsibility), assistant-ui's markdown primitive (the app doesn't render through assistant-ui components). *Amendment (7.1 observation pass): reasoning renders as one collapsible bubble PER segment, ordered with the tool cards via an explicit `parts` list on the message (`reasoning → tool → reasoning → text`), not as a single section pinned above the text — a turn's round-1 and round-2 reasoning are separate bubbles, each expanded only while it is the streaming tail. Live deltas, regeneration variants, and hydrated transcripts all build the same ordered body.*

**D7 — Tool display names.** A per-workspace module cache fetches the tools catalog once (`api.tools.list`) and exposes `displayName(id)`; `ToolCall` renders `displayName(t.name) ?? t.name` on the collapsed row and the raw id on the expanded card. The catalog is instance-static, so no invalidation beyond workspace switch.

**D8 — In-chat error entries.** New thread message shape `author: 'error'` carrying `{ error, ts }`, appended by the bridge's `onError` after `retractIfEmpty()`; rendered by a compact `ErrorEntry` component (danger styling, no actor avatar). Auth failures keep the existing D4-retry/connect-state path and never append an entry (spec'd). The empty-optimistic retraction already exists — the error entry makes the turn's outcome visible where the retracted row used to be.

**D9 — `session_events.payload` bytea → text.** Edit `000016_agent_runtime.up.sql` in place (`payload text NOT NULL`): the whole agent-runtime migration set is uncommitted, so in-place is clean **iff** no database has applied it; if one has, add a conversion migration instead (`USING convert_from(payload, 'utf8')`, down via `convert_to(payload::text, 'utf8')`) — existing rows are guaranteed JSON since `HumanReadableSerializer` is the only writer. `session_events.go` binds `string(e.Payload)` (pgx binds Go `[]byte` as bytea format, which errors against a text column); the scan side and the `[]byte` domain port stay unchanged, keeping the store layer Eino-free. `session_checkpoints.data` stays bytea.

## Risks / Trade-offs

- [Live args may be partial JSON — `emitToolStarted` dedups on the first frame chunk mentioning a call id, and eino may chunk tool-call args across frames] → Verify against a real streaming turn during implementation; if partial, accumulate chunks per call id before emitting, or take arguments from the completed assistant message's tool calls at `message_completed`.
- [Reasoning availability is provider/model-dependent] → No reasoning block renders nothing (spec'd); the reasoning-effort config gating in the agent modal already reflects model support.
- [Markdown re-parse cost during streaming] → Memoized components, chat-length messages; measure before adding throttling.
- [Error entries are not durable across reload] → Documented non-goal; revisiting requires a replay-safe persistence seam (run-status join is the leading candidate).
- [Edited in-place migration vs already-provisioned databases] → Checked before implementation: any DB at schema ≥ 000016 gets the conversion migration instead.
- [Hydration joins load full session history to build the maps] → `History` already loads all rows per call; the join adds one in-memory pass, no extra queries.

## Open Questions

- None blocking. The partial-args question (first risk) is answered by observation during implementation, not by a decision.
