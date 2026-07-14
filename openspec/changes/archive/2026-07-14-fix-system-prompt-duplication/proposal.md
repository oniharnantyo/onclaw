## Why

System-role messages — the agent instruction that Eino re-injects every turn,
plus middleware-injected system context such as the curated-memory block — are
being persisted into `conversation_messages` turn rows. Because they are also
re-injected on every subsequent turn, each past turn replays an extra copy of
the system prompt, on top of the fresh copy the framework injects for the
current turn. The net effect is `1 + N` copies of the (large) persona/system
prompt in the model input after `N` turns.

This is silent token bloat that directly conflicts with onclaw's low-resource
(~2 GB RAM) target and its conservative `max_context_tokens: 64000`. It is
visible in production traces: a 2-turn conversation ships 3 full copies of the
system prompt in a single model call.

## What Changes

- **Exclude system-role messages from turn persistence.**
  `HistoryMiddleware.accumulateNewMessages` SHALL skip any message with
  `role == system` when buffering a turn's new messages, so neither the
  framework's instruction nor middleware-injected system context is ever written
  to `conversation_messages`.
- **Single write-path guard.** `accumulateNewMessages` is the only funnel
  through which state-derived messages enter the persistence buffer (it is
  called from both `AfterModelRewriteState` and `AfterAgent`). One guard there
  closes both leak paths — the Eino instruction and the curated-memory block.
  No read-path change is required: once nothing writes system messages, replay
  never encounters them.

## Capabilities

### Modified Capabilities

- `conversation-history`: The "persisted as turn rows" requirement is tightened
  to explicitly exclude system-role messages from the persisted message array,
  with a scenario asserting no system message is stored.

## Impact

**Affected code:**
- `internal/agent/middlewares/history_middleware.go` — `accumulateNewMessages`
  (add a `role == system` skip with an explanatory comment)

**Affected systems:**
- Conversation history persistence and replay
- Per-turn token usage — `prompt_tokens` for follow-up turns drops by roughly
  `system-prompt-size × past-turn-count`, a meaningful reduction on the target
  device, and real conversation history is no longer crowded out

**Dependencies:**
- No new dependencies. This matches the pattern Eino's own `summarization`,
  `reduction`, and `dynamictool` middlewares already use — all skip
  `role == system` because system messages are transient by framework contract.

**Non-goals:**
- No migration of existing rows (stale data will be pruned by the operator).
- No read-path filtering (unnecessary once the write path is guarded).
- No change to *how* the instruction or curated memory is injected — only to
  *what is persisted*.
