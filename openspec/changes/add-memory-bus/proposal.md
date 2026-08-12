## Why

Memory background processes (KG entity extraction, dreaming/consolidation, episodic pruning) currently run inline and synchronously at session-end inside `FlushMessages`. This blocks the user from getting their prompt back until all LLM-heavy work completes. On a 2GB Pi with slow inference, this can add 5-10+ seconds of invisible latency. Additionally, adding new background consumers requires editing `FlushMessages` directly, violating open-closed principle.

An in-process message bus decouples event producers (the memory middleware) from consumers (KG extraction, Dreamer, pruner), making background work async, extensible, and independently testable.

## What Changes

- Introduce a lightweight in-process message bus (`internal/membus/`) with channel-based pub/sub
- Define memory domain events: `EpisodeCreated`, `DreamCompleted`, `ExtractionCompleted`
- Convert KG entity extraction from inline call to a bus worker
- Convert Dreamer (`MaybeDream`) from inline call to a bus worker
- Convert `PeriodicPruner` from standalone goroutine to a bus timer-worker
- Wire the bus into `AssembleAgent` with worker registration
- Add `CompactionSummary` assignment in the summarization Finalize callback (fixes existing wiring gap)
- Add `ShouldExtractEpisodic` signal gate to prevent trivial sessions from creating episodes
- `FlushMessages` publishes `EpisodeCreated` after the inline SQLite write instead of calling workers directly

## Capabilities

### New Capabilities
- `memory-bus`: In-process message bus for async memory background processing with typed events, worker registration, graceful shutdown, and backpressure via buffered channels

### Modified Capabilities
- `agent-memory`: Episodic capture is gated by a signal filter (`ShouldExtractEpisodic`) and the `CompactionSummary` is correctly wired from the summarization Finalize callback. KG extraction and dreaming become async via the bus rather than inline.

## Impact

- `internal/membus/` — new package (~150 lines)
- `internal/agent/middlewares/memory_middleware.go` — FlushMessages publishes events instead of calling workers inline
- `internal/agent/agent.go` — AssembleAgent wires bus, registers workers, manages bus lifecycle
- `internal/memory/episodic.go` — add `ShouldExtractEpisodic` function
- `internal/memory/dream.go` — Dreamer and PeriodicPruner adapted as bus workers (existing interfaces preserved)
- No new external dependencies (pure Go channels + sync primitives)
- No schema/migration changes (all existing stores unchanged)
- No breaking changes to public APIs
