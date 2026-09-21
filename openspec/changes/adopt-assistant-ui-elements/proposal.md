# Proposal: adopt-assistant-ui-elements

## Why

The assistant-ui elements gallery (https://www.assistant-ui.com/elements) is a mature catalog of chat-UX patterns, and OnClaw's gap against it is concentrated in one family the product most needs — context-window visibility — plus a set of structured-output and transcript elements. OnClaw's chat already ships ~12 of the gallery's patterns in its own design language; this change adopts the 16 missing ones, full-stack, so the context meter becomes a real diagnostic (ring + honest breakdown) and the model gains structured output lanes (todos, charts, previews, timelines) that render as first-class UI.

## What Changes

- **Context family (the core):** the chat-header bar meter moves into the composer's left rail as a token ring (pure assistant-ui severity: accent <65%, amber 65–85%, red >85%; no summarization-trigger tick), with an upward-opening popover hosting the **context breakdown**: face-value estimated segments (Instructions / Tools & skills / Files / Conversation — conversation counted only after the last compaction divider), a server-measured **Server context** row with provenance tooltip, derived Headroom, real last-turn input/output rows, and a count-up ticker on the used figure.
- **Backend context measurement:** the instruction composer reports section byte counts at compose time and `estimateWindowTokens` measures the true session window at turn end; usage events gain an optional `context_breakdown` block. The client estimate remains the fallback for old hydrated sessions.
- **Generative-UI registry:** a `$type`-keyed spec renderer (OnClaw tokens, plugin-extensible) that structured tool output renders through — built first; the elements below become its entries.
- **Echo tools (OnClaw's translation of assistant-ui's frontend tools):** new built-in Go tools `ui.chart`, `ui.timeline`, `ui.preview` that validate + echo their args; the transcript renders the envelope through the registry.
- **Todo list:** new `todo_write` write tool backed by a real `agent_todos` table (upsert-by-item-key transaction, rewrite semantics) + a checklist card (4 states, n/m · rev header); open items are one queryable state, not transcript archaeology. A compact open-items summary is injected into the agent's system context, with the full list available via `todo_read`.
- **Web search card:** dedicated renderer for the existing `web.search` tool — query pill, "Read N sources", domain avatars, staggered reveal.
- **Transcript elements:** message timing on hover (first token · total · tok/s), day separators + hover timestamps, a visible cancelable **message queue** above the composer while a run streams, and per-thread **draft restore**.
- **Tool use:** turns with ≥4 tool calls collapse into a "N steps · M files changed" timeline header that expands to the familiar inline cards (cards are never muted).
- **Math:** `$…$` / `$$…$$` LaTeX in assistant messages via remark-math + rehype-katex.
- **Deferred, not in scope:** the gallery's "conversation map" (element does not exist under that name; Flow-graph vs Search-in-conversation pick still owed) and the backend per-call measurement tier (T2 in `usageCapturingModel` — future upgrade behind the same wire field).

## Capabilities

### New Capabilities
- `web-app/generative-ui`: the `$type` spec-renderer registry and the structured elements that render through it — chart sparkline, web preview chrome (sandboxed iframe, URL bar, reload, open-in-new), timeline axis, todo checklist card, web search source card; streaming honesty rules (stagger reveal, no partial-parse claims).
- `agent-todos`: the `todo_write` / `todo_read` built-in tools, the `agent_todos` store (rewrite semantics, `UNIQUE (session_id, item_key)`, open-items index), catalog exposure, and open-items context injection.

### Modified Capabilities
- `web-app/chat`: composer gains the context ring + breakdown popover, message queue stack, and draft restore; transcript gains message timing, day separators/hover timestamps, and tool-group/timeline collapse behavior (≥4 calls).
- `workspace-tools`: new built-in catalog entries (`Todos`, `Chart`, `Timeline`, `Preview`) with per-agent exposure like all built-ins.
- `agent-runtime`: usage accumulation carries the optional `context_breakdown` block (compose-time section counts + end-of-turn window estimate).
- `openresponses`: the /v1 wire's usage block may carry `context_breakdown` (omitempty; additive, backward compatible).

## Impact

- **Frontend:** `web/src/components/chat/` (Composer, ChatHeader, ChatView, AgentMessage, new ContextMeter/ContextBreakdown/MessageQueue components), `web/src/lib/` (new `contextBreakdown.ts` estimator — replaces the parked scale-to-fit version — registry, toolDisplay entries), `web/src/store` (usage record carries turn input/output; queue + draft state), `web/src/chat/runtime.tsx` (usage pass-through, turn timing, ISO dates, queue dispatch).
- **Backend:** new `internal/agents/tools/todo.go` (+`ui_*.go` echo tools), `internal/agents/tool_catalog.go` registrations, `internal/agents/events.go` UsagePayload, instruction composer section counts, migration `000061_agent_todos`, a `storeport` store implementation + fake.
- **Dependencies:** remark-math + rehype-katex (~300KB gz) — the only new frontend package.
- **No breaking wire changes:** `context_breakdown` is omitempty; all existing fields and behaviors preserved. Tests: ChatHeader meter tests migrate to the composer side; toolDisplay coverage-guard pins new catalog entries.
