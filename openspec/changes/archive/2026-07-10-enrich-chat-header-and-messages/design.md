# Design — Enrich Chat Header and Messages

## Goals

- Surface already-computed token data and the agent context window in a global meter.
- Show when each message happened.
- Give the agent selector a prominent, stable home.
- Make the existing multi-line input discoverable.

## Non-goals

- Cumulative token accounting or cost estimation.
- Per-message token breakdowns (the meter is global only).
- Changing the multi-line key bindings.
- A typed citation/usage contract beyond what the meter reads.

## Key decisions

### D1. Meter "used" = latest turn `prompt_tokens`, not cumulative

Each turn resends the entire conversation history as the prompt, so a turn's `prompt_tokens` already represents "how full the context window is right now." Summing totals across turns double-counts (every prior turn's tokens are re-counted in the next prompt) and grows monotonically past the window size, which is meaningless as a fill indicator. Using the latest `prompt_tokens` against the context window gives an accurate, naturally-bounded meter that reflects summarization pressure (the agent already triggers summarization at 80% — `agent.go` `summarizationTrigger`).

### D2. Context window via the `init` SSE event

The context window is resolved once at `AssembleAgent` (passed as `contextWindow int`, used at line 218 for the summarization trigger). Storing it on the `Agent` struct + an accessor lets the handler include it in the `init` event, so the meter denominator is known before the first turn completes. `store.Agent` has no plain context-window field (it lives in `ModelMetadata` / config default `MaxContextTokens: 64000`), so re-resolving per request is avoided by carrying the assembled value.

### D3. Extend `TurnMeta`, reuse computed tokens

`history_middleware.go` already computes prompt/completion/total and persists them via `AppendTurn`. `TurnMeta` currently carries only `Tokens` (total). Adding `PromptTokens`/`CompletionTokens` to `TurnMeta` and populating them where the meta is built requires no new computation — only attaching values already in scope. The `turn` SSE event gains those fields; `Tokens`/`tokens` are retained for back-compat.

### D4. Meter works on history load too

`/api/conversations/:id/messages` serves `TurnRow`, which already serializes `prompt_tokens`/`completion_tokens`/`total_tokens`. The frontend sets `contextUsed` from the last turn's `prompt_tokens` and `contextWindow` from a `context_window` value returned with the payload. If resolving the context window from model metadata for a loaded conversation proves non-trivial, fall back to the config default (`MaxContextTokens`) — the streaming-driven meter (primary case via `init`/`turn`) is unaffected.

### D5. Timestamp on the optimistic user message

Server-loaded messages already carry `created_at`. The optimistic user message (`ChatProvider.tsx` `runChat`) currently omits it; stamping it with `new Date().toISOString()` at submit time lets the timestamp render immediately. Streaming assistant messages have no `created_at` until the post-stream reload; the timestamp render is gated on presence so it simply does not render during streaming.

### D6. Header bar, selector relocation, hint

A new `.chat-header-bar` row above `Thread.Viewport` (inside `thread-main`) follows the `.main-header` pattern. `AgentSelector` moves there (far-left); the meter sits right. The composer toolbar left side then holds only the attach control. The Shift+Enter hint is placeholder + fineprint text only — no key-handling change.

## Risks / edge cases

- `context_window === 0` (missing model metadata): meter hides rather than dividing by zero.
- Very long conversations: meter clamps at 100% (the bar fills; `used` may exceed `max` numerically but the bar is capped).
- Streaming message without `created_at`: no timestamp line until reload (acceptable; the streaming indicator conveys liveness).
- Back-compat: `TurnMeta.Tokens` and the `turn` event's `tokens` field are kept.
