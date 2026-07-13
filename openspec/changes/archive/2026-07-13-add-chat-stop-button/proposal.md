# Proposal: Add Stop Control to Running Chat

## Intent
Let the user cancel an in-progress agent stream from the chat composer. Cancellation is **frontend-only** (an `AbortController` on the existing `fetch`); the backend already terminates the run when the client disconnects because the handler threads `r.Context()` through the agent. A stopped turn's partial output is retained **in memory only** (kept visible, marked "stopped", not re-fetched) and is **not persisted** — preserving the codebase's existing "cancelled turns leave no trace" invariant.

## Problem
- While the agent is streaming, the composer's send affordance is a non-clickable spinner and `Composer.Send` is `disabled={isStreaming}`. There is **no way to stop** a running turn short of closing the tab.
- `ChatProvider.runChat` calls `fetch('/api/chat')` with **no `AbortController`**, so the client cannot signal cancellation to the server.
- The `ComposerCancel` primitive already exists (`web/src/components/primitives/Composer.tsx`, `disabled={!isStreaming}`) but is never rendered.
- Every stream — on success or failure — ends with `setTimeout(() => fetchMessages(convID), 200)`. Because a cancelled turn writes **zero** turn rows (the history middleware's single `AppendTurn` uses the now-cancelled request context; locked by `TestCancellationNonPersistence`), that re-fetch would erase the partial assistant text the user just watched stream in. The stop design must avoid this "vanish on stop".

## Proposed Solution
**Frontend (`web/src/`):**
- `ChatProvider.runChat`: create one `AbortController` per run, pass its `signal` to `fetch`; expose `stopChat()` that calls `controller.abort()`. Hold the controller in a `useRef` so `stopChat` is stable across renders.
- Branch the stream `catch` on `err?.name === 'AbortError'`:
  - **Abort** → `dispatch({ type: 'STREAM_STOPPED' })`; show **no** error toast; **skip** the post-stream `fetchMessages` re-fetch so the in-memory partial remains visible.
  - **Network/other error** → existing behavior (toast + `STREAM_ERROR`).
- Add a `stopped` flag to `ChatMessage` and a `STREAM_STOPPED` reducer action that clears `isStreaming` and marks the trailing assistant message stopped, so the UI can render a "(stopped)" affordance.
- `Chat.tsx` composer action toolbar: render `ComposerCancel` while `isStreaming`, `Composer.Send` while idle (toggle, since `Send` is disabled during streaming); wire `ComposerCancel.onClick` to `stopChat`.

**Backend (`internal/`):** No changes. Cancellation already propagates: client abort → Go cancels `r.Context()` → `eventIterator.Next()` short-circuits on `ctx.Err()` and the eino model layer aborts on `ctx.Done()` (proven by `internal/agent/agent_test.go::TestAssembleAndRunAgent_Cancellation`). No `/stop` endpoint is needed — closing the connection is the signal.

## Constraints & Dependencies
- **Invariant preserved:** Stopped turns are not persisted. `internal/agent/middlewares/history_middleware_test.go::TestCancellationNonPersistence` remains valid and unmodified.
- **Frontend-only change:** no DB schema, store-interface, SSE protocol, or backend change.
- **Headless-primitive architecture:** reuses the existing `ComposerCancel` primitive; no new primitive.
- **Partial retention is session-scoped:** a stopped partial lives only in React state. Reloading the page, or switching conversations and back (which re-fetches from the DB), drops it — intentional and consistent with non-persistence. The `stopped` marker sets the expectation visibly rather than failing silently.
- **Tool-in-flight caveat:** stop aborts model generation promptly (tested). If stop lands while a tool is executing, prompt termination depends on that tool honoring `ctx.Done()`; a long-running tool call may run to completion. Documented, not fixed here.

## Out of Scope (Deferred)
- **Persisting stopped partials to the DB** (progressive per-iteration upsert of the turn row + a `status` column + a sentinel `tool_result: "[interrupted]"` repair to avoid dangling-tool-call 400s on the next turn). Deferred until a trigger condition is met — see `design.md`:
  1. the chat log is used as a durable audit/work journal revisited across sessions, **or**
  2. crash/disconnect recovery is a real operational concern for the deployment, **or**
  3. a "resume interrupted run" product feature is wanted.
  Rationale: agent side-effects (file edits, shell commands, memory writes) already persist where they live; conversation persistence of a partial is documentation, not work-loss, and injecting a partial into the agent's next-turn context risks provider 400s and re-polluting wrong directions.
- A backend `/stop` endpoint (not needed; context cancellation suffices).
- Guaranteeing prompt cancellation of tools that do not respect `ctx.Done()`.
