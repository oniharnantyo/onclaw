## Context

**Current State:**
The agent is assembled with the persona/system prompt via Eino's `Instruction`
field (`internal/agent/agent.go`). Eino materializes `Instruction` into a real
`role: system` message in the agent's message stream on every turn
(Eino's own `agentic_test.go` asserts the system message lands in the message
list). Separately, `MemoryMiddleware` injects a `## CURATED LONG-TERM MEMORY`
system message every turn when curated core memory is enabled
(`memory_middleware.go`).

`HistoryMiddleware` persists a turn by accumulating every non-persisted message
in `state.Messages` into a buffer via `accumulateNewMessages`, then committing
that buffer once at turn end in `AfterAgent`. It has no filter for message role.

**Problem:**
Because system messages land in `state.Messages`, `accumulateNewMessages`
buffers them, `AfterAgent` commits them into the turn row, and `BeforeAgent`
replays them on the next turn — while Eino *also* re-injects a fresh system
message for that turn. After `N` turns the model input carries `1 + N` copies of
the system prompt. On the target device this is significant waste and crowds out
real conversation history.

**Constraints:**
- Must not change how the system prompt is *delivered* to the model (still
  injected fresh each turn).
- Must not break persistence of user/assistant/tool messages.
- Must match Eino's own treatment of system messages as transient.

## Goals / Non-Goals

**Goals:**
- Persist exactly zero system-role messages per turn.
- Keep the change to a single, well-justified location.
- Add a regression test asserting the invariant.

**Non-Goals:**
- Migrate or clean existing rows (operator prunes).
- Filter on the replay (read) path.
- Introduce a general "ephemeral message" tagging model.

## Decisions

### Decision 1: Filter by `role == system`, inside `accumulateNewMessages`

**Rationale:**
- `role` is the correct classifier. Every system message in this architecture is
  transient (re-injected per turn): the Eino instruction and the curated-memory
  block are both `role == system`.
- `accumulateNewMessages` is the single funnel for state-derived messages into
  the persistence buffer — called from both `AfterModelRewriteState` and
  `AfterAgent`. One guard there closes every leak path; the initial buffering in
  `BeforeAgent` only ever sees the new user input (it runs before
  `MemoryMiddleware`), so it needs no change.
- This is the framework-idiomatic fix: Eino's own `summarization`,
  `reduction`, and `dynamictool` middlewares all do
  `m.Role == schema.AgenticRoleTypeSystem → skip`.

**Alternatives Considered:**
- *Also filter on the read path (`BeforeAgent`).* Rejected as redundant: once the
  write path never stores system messages, replay never sees them. Read-side
  filtering would only matter for pre-fix rows, which the operator will prune.
- *Explicit "ephemeral" tag on injected messages.* Rejected (YAGNI): more
  general, but Eino's instruction message cannot be tagged by onclaw, so a
  role-based fallback would still be required underneath — moving parts for no
  current capability gain. Revisit if a legitimate "persistent system note"
  feature is ever added.
- *Stop using Eino `Instruction`; inject the system message ourselves as a
  flagged message.* Rejected: reinvents a framework feature and risks losing
  other `Instruction`-driven behavior — a large change for a one-line fix.

### Decision 2: No migration, no read-path change

**Rationale:**
- The operator will prune stale conversations, so existing rows that contain
  system messages are not a concern.
- With the write path guarded, the read path is correct by construction.

## Risks / Trade-offs

**Risk:** A future feature wants a system-role message to persist (e.g. a
user-authored persistent system note).
- **Mitigation:** The role-based filter encodes today's invariant explicitly and
  is documented in the spec and in a comment at the guard site. If that feature
  arrives, move toward an explicit ephemeral tag (which generalizes), keeping
  the role filter as a backstop.

**Risk:** Some provider or middleware emits a `role: system` message carrying
turn-essential content.
- **Impact/Mitigation:** None today — all system content is re-injected each
  turn, so dropping the stored copy loses nothing. The regression test asserts
  user/assistant/tool messages are still persisted.

**Trade-off:** Follow-up turns report lower `prompt_tokens` (no duplicated
system prompt). This is the intended, beneficial effect.

## Open Questions

None. Confirmed with the operator: stale data will be pruned; no migration is
needed.
