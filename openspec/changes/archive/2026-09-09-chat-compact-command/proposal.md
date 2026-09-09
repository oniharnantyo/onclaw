## Why

The composer's slash menu is prototype-era fiction: `/tools`, `/model`, `/schedule`, `/reset`, `/help` insert plain text that reaches the model as an ordinary user message, so `/tools` literally asks the model "what is /tools?". Meanwhile compaction is invisible end-to-end — the eino summarizer fires at threshold, the runner mints a `context_compacted` transcript event, but the event is never forwarded on the /v1 wire and the web never renders it. Users get no manual lever on context and no signal when the context was rewritten under them. Competitors (OpenClaw `/compact`, Hermes `/compress`) treat manual compaction as table stakes.

## What Changes

- **BREAKING** — Slash commands reduced to a single real command. The command menu lists only `/compact`. The scripted-reply commands (`/tools`, `/model`, `/schedule`, `/reset`, `/help`) are deleted; an unknown `/foo` sends as ordinary text (OpenClaw web behavior).
- **`/compact [focus]`** — a real command intercepted in the composer, available in agent chats only (not channels). Optional text after the command steers the summary. The command itself never becomes a user message.
- **Compact-as-a-turn** — the composer submits the request as a normal /v1 turn carrying `metadata.onclaw_command: "compact"` with the focus text as `input`. The run pipeline's active-run guard serializes compaction against running turns.
- **Runner compact execution** — for a compact-command turn the runner skips the normal model turn: it loads the session messages, runs a standalone eino summarization instance (agent's own model, same transcript-offload callback, `UserInstruction` = focus text), appends the `SessionEventMessagesReplaced` record, and emits `context_compacted` (now carrying before/after token estimates) plus `turn_completed` (summarizer-call usage) on the existing stream.
- **Wire delivery** — /v1 streams a new custom `onclaw:context_compacted` SSE event (tokens before → after); the catch-up attach and history replay paths deliver it too, so hydrated transcripts show past compactions.
- **Web rendering** — a "Compacting context…" status row replaces the command text while the turn runs; on completion the transcript shows a `Context compacted · N → M tokens` divider. The same divider renders for automatic threshold compactions, closing the invisible-compaction gap. No user pill for the command, no agent ack message.
- Skills remain `$`-invocation only; nothing skill-related enters the `/` menu.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/chat`: the Slash-commands requirement is replaced — menu lists only `/compact`, commands are intercepted client-side and never reach the model as text. New requirements: compact status row, compaction divider rendering (manual and automatic), divider hydration on reload.
- `openresponses`: requests may carry `metadata.onclaw_command: "compact"` (focus text in `input`); the streaming event contract gains `onclaw:context_compacted`; catch-up attach and history replay deliver the event.
- `agent-runtime`: the Context-summarization requirement extends to a runner-executed manual path (standalone summarization instance, `UserInstruction` focus steering, `SessionEventMessagesReplaced` persistence) and the context-compacted transcript event carries before/after token estimates.

## Impact

- Backend: `internal/agents` (ExecRequest command field, compact-turn execution in the runner, CompactionPayload fields, standalone summarization instance), `internal/server/handlers/v1.go` (metadata command routing, SSE event translation), possibly `internal/server` catch-up/history paths.
- Frontend: `web/src/lib/constants.ts` (COMMANDS gutted), `web/src/components/chat/Composer.tsx` (interception, agent-chat gating), `ChatView.tsx`/`livechat.ts`/`openresponses.ts` (compact turn submit, `onclaw:context_compacted` handling, status row, divider), `AgentMessage`/history hydration untouched (backend already replays `context_compacted`).
- No migrations; no wire breaking change (new event type is additive; clients ignoring unknown event types stay valid).
- Dependencies: none new — uses the already-imported `eino/adk/middlewares/summarization`.
