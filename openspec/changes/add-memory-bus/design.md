## Context

The memory middleware (`internal/agent/middlewares/memory_middleware.go`) currently executes all post-episodic work synchronously in `FlushMessages`:

1. `ExtractAndFlush` — writes L0 archive documents (inline, needs completion)
2. `SummarizeSession` — produces episodic summary (inline, needs result)
3. `AppendEpisodic` — SQLite write (inline, needs episode ID)
4. `extractAndIngestEntities` — LLM call + KG write (blocking, expensive)
5. `MaybeDream` — threshold check + possible LLM call + core write (blocking, expensive)

Steps 4-5 can take 5-10+ seconds on constrained hardware. The `PeriodicPruner` runs as a separate goroutine started directly on the Agent struct. There is no unified lifecycle for background memory workers.

Additionally, two wiring gaps exist:
- `CompactionSummary` field is never assigned, causing redundant LLM calls
- `ShouldExtractEpisodic` is tested but not implemented, allowing trivial sessions to create noisy episodes

## Goals / Non-Goals

**Goals:**
- Decouple episodic event production from background consumer work (KG extraction, dreaming, pruning)
- Reduce session-end latency by moving expensive LLM calls off the critical path
- Unify lifecycle management of all memory background workers under one construct
- Fix `CompactionSummary` wiring so long sessions avoid redundant summarization LLM calls
- Implement the `ShouldExtractEpisodic` signal gate to filter trivial sessions
- Make it trivial to add future memory workers without editing `FlushMessages`

**Non-Goals:**
- External message brokers (Kafka, NATS, Redis) — in-process only
- Distributed or multi-process bus — single-process, single-binary constraint
- Changing the memory storage layer (SQLite schema, stores unchanged)
- Modifying the hooks dispatcher — it serves a different purpose (user shell hooks, blocking)
- Per-turn event publishing — only session-boundary events

## Decisions

### D1: Channel-based in-process bus over goroutine-per-event

**Choice:** Single buffered channel (`chan Event`) with one dispatcher goroutine that fans out to registered workers.

**Alternatives considered:**
- Goroutine-per-publish: simpler but risks goroutine explosion if workers are slow; no backpressure
- sync.Cond + queue: more complex, same semantics as buffered channel
- Existing hooks.Dispatcher: wrong semantics (blocking, audited, shell-exec); mixing internal async work into it would violate its contract

**Rationale:** A single dispatcher goroutine gives ordered processing, bounded memory (channel buffer), natural backpressure (publish blocks only when buffer full), and predictable shutdown. On a 2GB Pi, goroutine explosion is unacceptable.

### D2: Buffer size of 64 events

**Choice:** `make(chan Event, 64)`

**Rationale:** Even the most active session produces at most 1 `EpisodeCreated` event. The buffer handles bursts from rapid session cycling (e.g., automated tests) without blocking the publisher. 64 is generous but costs only ~4KB.

### D3: Workers process events sequentially per-worker, parallel across workers

**Choice:** Each worker gets its own goroutine. The dispatcher fans out each event to all matching workers concurrently. Within a single worker, events are processed sequentially (worker's own channel).

**Rationale:** KG extraction and Dreamer are independent — they can run concurrently. But a single worker (e.g., Dreamer) should not process two events simultaneously (debounce logic depends on sequential access). Per-worker channels provide this naturally.

### D4: Non-fatal worker errors — log and continue

**Choice:** Worker errors are logged via `slog.Warn` but never propagate to the session or block future events.

**Rationale:** Memory consolidation is best-effort. A failed KG extraction should not prevent the Dreamer from running or crash the agent. This matches the existing `_ = m.Dreamer.MaybeDream(ctx)` pattern (error discarded).

### D5: Bus lifecycle owned by Agent, started in AssembleAgent

**Choice:** The bus is created and started in `AssembleAgent`, stored on the `Agent` struct, and stopped (with drain) on agent shutdown.

**Rationale:** The bus lifetime exactly matches the agent lifetime. Workers need stores and models that are resolved during assembly. Starting the bus there ensures all dependencies are available.

### D6: CompactionSummary assigned in Finalize callback

**Choice:** The summarization middleware's `Finalize` callback assigns `memoryMiddleware.CompactionSummary = summaryText` before returning reconstructed messages.

**Rationale:** The Finalize callback already has the summary message. A single field assignment (1 line) closes the wiring gap. The `onStopFlush` path already reads `CompactionSummary` — it just never has a value today.

### D7: ShouldExtractEpisodic uses heuristic signal detection

**Choice:** A simple heuristic function that checks:
1. If `compactionSummary != ""` → always extract (session was long enough to compact)
2. If message count ≤ 2 AND all messages are short (< 50 chars) → skip
3. If messages contain tool calls, durable-signal keywords ("remember", "prefer", "always"), or substantial content → extract

**Alternatives considered:**
- LLM-based relevance check: too expensive for the Pi (defeats the purpose)
- Always extract: creates noise from "Hello" / "Hi" sessions
- Token-count threshold: brittle, doesn't capture semantic signal

**Rationale:** Matches the existing test expectations (`TestShouldExtractEpisodic`). Cheap (string checks, no LLM). Conservative — when in doubt, extract (false positives are pruned by TTL).

### D8: Event types are a sealed set of structs implementing an interface

**Choice:** `type Event interface { EventName() string }` with concrete structs per event.

**Rationale:** Type-safe dispatch. Workers declare which event names they handle. No runtime reflection or type assertions in the hot path. Adding a new event = adding a new struct (open for extension).

## Risks / Trade-offs

- **[Risk] Events lost on crash** → Acceptable. Episodic data is already written to SQLite inline before publishing. The bus only carries "please do background work" signals. On restart, the Dreamer will eventually catch up (threshold check is idempotent).

- **[Risk] Worker blocks indefinitely (slow LLM)** → Mitigation: per-worker context with timeout (default 60s for LLM workers). Timeout cancels the in-flight LLM call. Event is logged as failed and dropped.

- **[Risk] Channel full (producer blocks)** → Mitigation: with buffer=64 and at most 1 event per session, this only happens under extreme conditions (automated load test). If full, `Publish` logs a warning and drops the event (non-blocking publish with select/default).

- **[Trade-off] Ordered processing per-worker reduces parallelism** → Acceptable. The Dreamer must process episodes sequentially (debounce state). KG extraction has no ordering requirement but gains simplicity from sequential processing within its worker.

- **[Trade-off] Two event systems (hooks.Dispatcher + membus.Bus)** → Intentional. They serve fundamentally different purposes: hooks are user-facing, synchronous, blocking, audited; the bus is internal, async, fire-and-forget, logged. Merging them would violate both contracts.
