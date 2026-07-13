# Design: Add Stop Control to Running Chat

## Decision: in-memory retention, not persistence

The stop control retains a stopped turn's partial output **in memory only** and does **not** persist it. This deliberately preserves the codebase's existing invariant — a cancelled turn leaves no trace — rather than reversing it.

### Why the backend needs no work
The handler threads the HTTP request context through the agent:

```
client abort  →  r.Context() cancelled  →  eventIterator.Next() sees ctx.Err() → returns false
                                        →  eino model layer aborts on ctx.Done()
```

Both termination paths are proven by existing tests:
- `internal/agent/agent_test.go::TestAssembleAndRunAgent_Cancellation` — cancelling the context mid-stream surfaces a cancellation error and stops the model stream.
- `internal/agent/middlewares/history_middleware_test.go::TestCancellationNonPersistence` — a cancelled context yields zero persisted turns (the single `AfterAgent` `AppendTurn` uses the dead context and fails).

Closing the connection (which `AbortController.abort()` does) is therefore a complete stop signal. A `/stop` endpoint would be redundant.

### Why not persist the partial (the deferred option)
Persisting stopped partials would require: per-iteration upsert of the turn row (the middleware currently commits once at `AfterAgent`), a `status` column, and a sentinel `tool_result: "[interrupted]"` repair to prevent a dangling tool-call from causing a provider 400 on the next turn. That complexity is deferred because:

1. **Agent side-effects already persist.** File edits, shell commands, staged writes, and memory-store writes are durable in the filesystem / their own stores regardless of whether the conversation row records them. Non-persistence loses the *narrative*, not the *work*.
2. **The dominant stop reason argues against context pollution.** Users typically stop because the agent is going the wrong way. Injecting that partial into the next turn's context re-pollutes the reasoning the user just rejected. The current "stopped turn never happened" semantic gives a clean retry slate.
3. **Crash recovery is rare on the target surface.** onclaw serves localhost / LAN on a single-board computer; the case that truly needs durability (involuntary disconnect) is uncommon, and the complexity tax (schema change, write amplification on ~2 GB RAM, sentinel repair) is high relative to that benefit.

### Trigger conditions for revisiting persistence
Re-open the persistence path as a separate change if any of these becomes true:
1. The chat log is used as a durable audit / work journal revisited across sessions.
2. Crash/disconnect recovery becomes an operational concern for the deployment.
3. A "resume interrupted run" product feature is wanted (the only case that truly needs the partial in the agent's context).

### Compatibility with existing spec
The chat-ui spec's "Streaming Tool-Call Rendering" requirement states that the post-completion re-fetch replaces the streamed bubble with the authoritative merged message. The stop path is compatible: the re-fetch-skip and `stopped` marker apply **only** to the abort path; normal completion still re-fetches and reconciles as before.

### Tool-in-flight caveat
Stop aborts model generation promptly. If stop arrives while a tool is executing, prompt termination depends on that tool honoring `ctx.Done()`; a long-running tool may run to completion. This is documented in the proposal's constraints and is not addressed by this change.
