# Tasks

## 1. Backend: surface context-window and turn tokens

- [x] 1.1 Add `PromptTokens`/`CompletionTokens int64` (JSON-tagged) to `store.TurnMeta` in `internal/store/types.go`; keep `Tokens`.
- [x] 1.2 In `internal/agent/middlewares/history_middleware.go`, populate `PromptTokens`/`CompletionTokens` on the built `store.TurnMeta` from the already-computed `prompt`/`completion` values (verify they are currently passed only to `AppendTurn`).
- [x] 1.3 Add `contextWindow int` field to the `Agent` struct in `internal/agent/agent.go`; assign it in `AssembleAgent` from the existing `contextWindow` param; add `func (a *Agent) ContextWindow() int`.
- [x] 1.4 In `internal/api/handler/chat.go`, extend the local `chatInitEvent` with `ContextWindow int64` (+ agent name) and the `turnSSEEvent` with `PromptTokens`/`CompletionTokens`/`TotalTokens`; populate from `assembledAgent.ContextWindow()` and `meta`.
- [x] 1.5 Ensure `/api/conversations/:id/messages` serializes `prompt_tokens`/`completion_tokens`/`total_tokens` (already on `TurnRow`) and returns `context_window` (resolve from agent model metadata, else config default `MaxContextTokens`).
- [x] 1.6 Add/update Go tests (black-box `_test`): `TurnMeta` carries prompt/completion; `Agent.ContextWindow()` accessor; SSE payload shape (init has `context_window`, turn has prompt/completion/total). Maintain ≥70% statement coverage on touched packages.

## 2. Frontend: types and state

- [x] 2.1 In `web/src/types/chat.ts`, add `context_window: number` to `SSEInitEvent`; add `SSETurnEvent` (`prompt_tokens`/`completion_tokens`/`total_tokens`/`tokens`).
- [x] 2.2 In `web/src/components/ChatProvider.tsx`, add `contextWindow`/`contextUsed` to `ChatState` + `SET_CONTEXT_WINDOW`/`SET_CONTEXT_USED` actions; reset both on `SET_ACTIVE_CONV_ID`.
- [x] 2.3 Handle `event === 'turn'` in the SSE loop → dispatch `contextUsed = data.prompt_tokens ?? data.total_tokens`; extend `init` handling → dispatch `context_window`.
- [x] 2.4 Stamp the optimistic user message with `created_at: new Date().toISOString()` in `runChat`.
- [x] 2.5 In `fetchMessages`, set `contextUsed` from the last turn's `prompt_tokens` and `contextWindow` from the payload's `context_window`.

## 3. Frontend: header bar, meter, timestamps, hint

- [x] 3.1 Add `formatMessageTime(dateStr)` (date + clock, e.g. `Jul 9, 14:32`) to `web/src/components/Chat.tsx`; render a `<time>` line above each `Message.Root` only when `msg.created_at` is present.
- [x] 3.2 Add a `ContextMeter` component reading `contextUsed`/`contextWindow` from `useChat()`: `used / max` (humanized, e.g. `12.4k / 64k`) + a progress bar (`width: pct%`, capped at 100%). Hide when `contextWindow === 0`.
- [x] 3.3 Add a `ChatHeaderBar` row at the top of `thread-main` (before the viewport wrapper): far-left `<AgentSelector />`, far-right `<ContextMeter />`.
- [x] 3.4 Remove `<AgentSelector />` from the composer toolbar (`Chat.tsx:317`); leave only `<Composer.Attach />` on the left.
- [x] 3.5 Update the composer placeholder to `Ask agent to perform a task… (Shift+Enter for new line)` and append `Enter to send, Shift+Enter for new line.` to the fineprint.
- [x] 3.6 Add CSS in `web/src/index.css`: `.chat-header-bar` (modeled on `.main-header`), `.context-meter` (bar + label, `--accent` fill, `--text-muted` label), `.message-time` (small `--text-muted`). Keep `.composer-agent-select` valid in the new location.

## 4. Verification

- [x] 4.1 `make vet && make test`; ≥70% coverage on touched `internal/...` packages.
- [x] 4.2 `cd web && npx tsc --noEmit && npm run build`.
- [x] 4.3 Manual E2E (`make run` + web dev): meter updates after a turn; denominator matches the agent context window; meter shows on history load; each message shows date/time (optimistic user msg shows immediately); agent selector is top-left; composer left holds only attach; Shift+Enter inserts a newline and the textarea grows; placeholder/fineprint mention it.
- [x] 4.4 Edge cases: meter hides when context window is 0; long conversations clamp the bar at 100%; streaming assistant message shows no timestamp until reload.
