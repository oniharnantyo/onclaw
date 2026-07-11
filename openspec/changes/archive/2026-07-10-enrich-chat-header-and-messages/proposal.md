# Enrich Chat Header and Messages

## Why

The chat surface shows messages as bare content blocks with no when-they-happened context and no sense of how much of the agent's context window the conversation is consuming. The agent selector is buried in the composer toolbar, where it competes for space with the attach control, and multi-line input works but is undiscoverable. Meanwhile the backend already computes per-turn token usage (`history_middleware.go` → `TurnRow.prompt_tokens/completion_tokens/total_tokens`) and the agent's context window is resolved at assembly — but neither reaches the UI. The `turn` SSE event carrying token data is emitted by the backend and **silently dropped by the client** (`ChatProvider.tsx` handles only `init`/`message`/`error`).

This change surfaces that existing data: a global context-usage meter and per-message timestamps, a top-of-thread header bar that moves the agent selector to a prominent far-left position, and a discoverability hint for the already-working Shift+Enter multi-line input.

## What Changes

- Add a **chat header bar** across the top of the main thread column (above `Thread.Viewport`), following the existing `.main-header` CSS pattern. The **agent selector moves here, far-left**; the **context meter** sits on the right.
- Add a **context-window usage meter**: a progress bar + `used / max` readout where `used` is the latest turn's `prompt_tokens` (the truest context-fill, since each turn resends the full history) and `max` is the agent's resolved context window. Cumulative totals are intentionally NOT used — they double-count and drift past the window.
- Surface the data the meter needs:
  - Store the resolved `contextWindow` on the `Agent` struct at assembly and expose a `ContextWindow()` accessor; include it in the `init` SSE event so the denominator is known before the first turn.
  - Extend `store.TurnMeta` with `PromptTokens`/`CompletionTokens` (already computed by the history middleware, currently only persisted to `TurnRow`) and include them in the `turn` SSE event.
  - Attach per-turn token counts (already in `TurnRow`) and the context window to the `/api/conversations/:id/messages` payload so the meter also shows on history load.
- Add **per-message timestamps** rendered above each message bubble (date + clock, e.g. `Jul 9, 14:32`). The optimistic user message gets a client-side `created_at` at submit time; server-loaded history already carries `created_at`.
- Add a **Shift+Enter discoverability hint** in the composer placeholder and fineprint. The multi-line behavior (`Enter` sends, `Shift+Enter` inserts a newline) already works per the existing `Multi-line Composer Input` requirement — this is text only, no behavior change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-ui`: adds a chat header bar with the relocated agent selector, a context-window usage meter, per-message timestamps, the data plumbing (SSE + messages payload) the meter requires, and a discoverability hint for the existing multi-line composer input.

## Impact

- **Backend (Go)**: `internal/store/types.go` (`TurnMeta` gains `PromptTokens`/`CompletionTokens`); `internal/agent/agent.go` (store + expose `contextWindow`); `internal/agent/middlewares/history_middleware.go` (populate new `TurnMeta` fields from already-computed values); `internal/api/handler/chat.go` (extend `init`/`turn` SSE event payloads); the messages handler/service that serializes `TurnRow` (include token fields + `context_window`).
- **Frontend (React/TS)**: `web/src/types/chat.ts` (`SSEInitEvent` gains `context_window`; new `SSETurnEvent`); `web/src/components/ChatProvider.tsx` (handle the `turn` event, new `ChatState` fields + actions, `created_at` on the optimistic user message, meter data on history load); `web/src/components/Chat.tsx` (header bar + `ContextMeter`, move `AgentSelector`, timestamp rendering, placeholder/fineprint hint); `web/src/index.css` (`.chat-header-bar`, `.context-meter`, `.message-time`).
- **No database migration, no new dependencies, no change to memory/secret/provider layers.** All token data is already computed and stored; this change only surfaces it.
- Tests: Go black-box `_test` packages (≥70% statement coverage on touched `internal/...` packages); `tsc --noEmit` + `npm run build` for the web app; manual E2E for streaming meter update, history-load meter, timestamps, selector relocation, and multi-line behavior.
