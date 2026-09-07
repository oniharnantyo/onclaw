## Why

The live chat transcript under-reports what actually happened in a turn, in both directions: real signal is dropped (reasoning never streams or renders, tool arguments are discarded at the stream boundary, tool results never reach the card during streaming because the client listens for a top-level event type the wire never sends, hydrated cards lose arguments/results/latency because the history projection never joins spans to their messages), and fabricated content is shown instead (the tool card renders a canned `ok — Nms, M rows` line whenever no result arrived, which is every live card). Errors surface only as toasts, markdown renders as plain text, and tool cards show raw ids where the tools catalog already provides display names. A turn's transcript should faithfully show what the model thought, called, got back, and failed on.

## What Changes

- Reasoning flows end-to-end: the runner extracts eino reasoning content blocks and emits the already-defined reasoning-delta transcript event; the `/v1` stream carries `onclaw:reasoning_delta`; the web runtime accumulates it; the chat renders it as a collapsible thinking section on the agent message (live and hydrated).
- Tool-call fidelity on the wire: `function_call` output items carry their arguments at `output_item.added`; the tool trace item shape is pinned as `onclaw.function_call_output` items delivered via the standard `response.output_item.added`/`response.output_item.done` events (present in aggregated `response.output` too), carrying call id, tool name, result, `latency_ms` when measured, and `is_error` on failure. This amends the spec's previous `onclaw:function_call_output` (colon) wording to match the shipped, test-pinned shape.
- Tool result latency is measured: the runner stamps finished tool events with the call's measured duration so the wire, history, and UI show real latencies instead of `0ms`.
- Hydration fidelity: the session history projection joins persisted spans to their assistant/tool-result messages so transcript reads carry tool arguments, results, latency, and assistant reasoning content.
- Web client event dispatch: `runTurn` handles reasoning deltas, reads tool arguments from `function_call` items, and routes tool outputs from `onclaw.function_call_output` items — removing the dead top-level `onclaw.function_call_output` case.
- Chat rendering: agent text renders as markdown (preserving `@mention` highlighting); tool cards show catalog display names with the raw id retained; completed cards with no result render an explicit empty state instead of a fabricated string; failed turns append an error entry to the transcript (toasts remain for connect/transient feedback, the connect state keeps governing auth/key failures).
- Storage debuggability: `session_events.payload` changes `bytea` → `text` so payloads are readable in SQL without casting (no spec-level behavior change; the log stays opaque to queries).

## Capabilities

### New Capabilities

- _None._

### Modified Capabilities

- `web-app/chat`: transcript rendering — markdown rendering of agent text, collapsible reasoning display, human-readable tool names, honest completed-card empty state, and in-transcript turn error entries.
- `web-app/chat-runtime`: streaming lifecycle — reasoning deltas accumulate onto the agent message, tool cards capture real arguments/results/latency from the stream, failed turns surface an error entry in the thread, and hydration carries the same fidelity fields.
- `openresponses`: streaming event contract and server-side tool trace — `onclaw:reasoning_delta` passthrough, `function_call` items carry arguments, tool trace pinned to `onclaw.function_call_output` items with result/latency/is_error.
- `agent-runtime`: session history — the transcript read projects arguments, results, latency, and reasoning content by joining persisted spans to their messages.

## Impact

- **Go backend:** `internal/agents/runner.go` (reasoning extraction, latency measurement), `internal/agents/history.go` (span→message joins for args/results/latency/reasoning), `internal/agents/events.go` (extraction helpers), `internal/openresponses/translate.go` (no shape change; reasoning already maps), `internal/store/postgres/session_events.go` (text binding), `migrations/000016_agent_runtime.up.sql` (payload `bytea` → `text`; the migration set is uncommitted so it is edited in place unless a database has already applied it, in which case a follow-up migration converts via `convert_from`).
- **Web app:** `web/src/lib/openresponses.ts` (event dispatch), `web/src/chat/runtime.tsx` (reasoning/error/args accumulation), `web/src/components/chat/AgentMessage.tsx` + `ToolCall.tsx` (markdown, reasoning UI, display names, honest states), tools catalog fetch for display names (endpoint already exists).
- **Tests:** translator and runner/history Go tests; web suites for the runtime bridge and chat components (touched suites only — the repo has pre-existing jsdom localStorage failures unrelated to this change).
- **Seeded demo data:** seeded tool cards gain result text so cards stay complete once the fabricated fallback is removed.
