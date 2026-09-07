# Proposal: chat-context-meter

## Why

The runtime already manages the context window (summarization fires at 75% of the resolved window) and reports per-turn token usage on every terminal response event, but none of this is visible to users: the chat client drops the usage block, and nothing shows how full an agent's context window is until summarization silently rewrites the thread. A context meter surfaces both, turning an invisible mechanism into a legible one.

## What Changes

- Add a **context meter to the chat header** (top right, left of existing controls): a compact bar plus mono percentage showing how much of the agent's context window the conversation currently fills; hover shows exact tokens (`68k / 200k`). Rendered only for 1:1 agent chats — channels and team DMs show no meter.
- Meter value = **input tokens of the last model call** in the most recent turn ÷ effective context window. The summed per-turn input tokens (which multi-count context across ReAct iterations) stay reserved for billing/Runs display.
- Runner usage accounting gains a **final-model-call input count** alongside the existing sums; it rides terminal `/v1` response events (`response.completed` / `response.incomplete` / `response.failed`) and the persisted transcript's turn events, so the meter updates live and survives reload.
- The **agents API exposes two computed, read-only fields** on the agent payload: `effective_context_window` (stored `context_window` or the 200,000 default — exactly what execution resolves) and `summarization_trigger_tokens` (effective window × the server's summarization margin). The client renders the server's numbers and holds no resolution logic or threshold constants.
- Meter turns **amber at the summarization trigger line** — the same threshold where the server compacts — so a post-compaction drop is self-explanatory.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: turn usage accounting additionally records the final model call's input tokens; the history read path rebuilds it from persisted span events so transcripts carry it after reload.
- `openresponses`: the terminal Response `usage` block additionally carries the final-model-call input token count for the executed turn.
- `agents`: the agent payload exposes `effective_context_window` and `summarization_trigger_tokens` as computed read-only fields mirroring runtime resolution.
- `web-app/chat`: chat header renders a per-agent context meter fed by terminal usage events and the agent payload, with neutral/warn/hidden states.

## Impact

- **Backend:** `internal/agents` (runner usage accumulation, `UsagePayload`, history rebuild), `internal/openresponses` (wire `Usage` struct + translation), `internal/server/handlers/agents.go` (two computed response fields).
- **Web:** `web/src/lib/openresponses.ts` (stop dropping `usage`), `web/src/lib/livechat.ts` (hydrate from turn events), thread/runtime state (per-thread meter value), `ChatHeader.tsx` (meter component, top-right placement).
- **API:** additive only — one new field in the `/v1` usage block, two new fields on agent payloads. No breaking changes.
- **Config:** none — the summarization margin stays a server constant; the client learns the effective trigger from the API (deliberately: surface-only, no new env/flag in this change).
