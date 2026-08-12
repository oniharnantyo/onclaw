## 1. Bus Core (`internal/membus/`)

- [x] 1.1 Create `internal/membus/event.go` — `Event` interface with `EventName() string` method
- [x] 1.2 Create `internal/membus/events.go` — concrete event types: `EpisodeCreated`, `DreamCompleted`, `PruneTick`
- [x] 1.3 Create `internal/membus/worker.go` — `Worker` interface with `Handle(ctx, Event) error` and `Subscribes() []string`
- [x] 1.4 Create `internal/membus/bus.go` — `Bus` struct with `New(bufferSize int)`, `Register(Worker)`, `Publish(Event)`, `Start(ctx)`, `Stop()`
- [x] 1.5 Implement per-worker dispatch goroutines with per-worker buffered channels
- [x] 1.6 Implement non-blocking publish with select/default and slog warning on drop
- [x] 1.7 Implement graceful shutdown: signal stop, drain buffer with timeout, wait on worker goroutines
- [x] 1.8 Create `internal/membus/timer.go` — `TimerWorker` adapter that publishes tick events at a configured interval
- [x] 1.9 Write unit tests for bus: publish/subscribe, fan-out, non-blocking, shutdown drain, drop on full

## 2. Memory Workers

- [x] 2.1 Create `internal/membus/kg_worker.go` — `KGExtractionWorker` subscribing to `episode_created`, calling `ExtractEntitiesWithSecurity` + `IngestExtraction` + `DedupAfterExtraction`
- [x] 2.2 Create `internal/membus/dreamer_worker.go` — `DreamerWorker` subscribing to `episode_created`, calling `Dreamer.MaybeDream`
- [x] 2.3 Create `internal/membus/pruner_worker.go` — `PrunerWorker` subscribing to `prune_tick`, calling `EpisodicStore.PruneExpired`
- [x] 2.4 Write unit tests for each worker: correct subscription, event handling, error non-propagation

## 3. Signal Gate (`ShouldExtractEpisodic`)

- [x] 3.1 Implement `ShouldExtractEpisodic(compactionSummary string, messages []*schema.AgenticMessage) bool` in `internal/memory/episodic.go`
- [x] 3.2 Verify the existing `TestShouldExtractEpisodic` test passes (skips routine, captures durable, captures work, captures compaction)
- [x] 3.3 Integrate `ShouldExtractEpisodic` into `FlushMessages` — wrap the episodic block with the gate check

## 4. CompactionSummary Wiring

- [x] 4.1 In `buildMiddleware` (agent.go), assign `memoryMiddleware.CompactionSummary` from the summary message text inside the `Finalize` callback
- [x] 4.2 Write a test verifying that after compaction fires, `CompactionSummary` is non-empty
- [x] 4.3 Write a test verifying that `FlushMessages` with non-empty `CompactionSummary` skips the LLM summarization call

## 5. Integration: Wire Bus into Agent Assembly

- [x] 5.1 Add `Bus *membus.Bus` field to the `Agent` struct
- [x] 5.2 In `AssembleAgent`, create the bus, register workers (KG, Dreamer, Pruner), and call `bus.Start(ctx)`
- [x] 5.3 Remove the standalone `PeriodicPruner` goroutine from `AssembleAgent` (replaced by PrunerWorker on bus)
- [x] 5.4 Add `Bus *membus.Bus` field to `MemoryMiddleware` and pass it during construction
- [x] 5.5 Refactor `FlushMessages`: after `AppendEpisodic`, publish `EpisodeCreated` event instead of calling `extractAndIngestEntities` and `MaybeDream` inline
- [x] 5.6 Ensure `Agent` shutdown path calls `bus.Stop()` (graceful drain before process exit)
- [x] 5.7 Write integration test: assemble agent with bus, run a session, verify episodic row exists and KG extraction ran via bus

## 6. Cleanup and Verification

- [x] 6.1 Remove inline `extractAndIngestEntities` call from `FlushMessages` (now handled by KG worker)
- [x] 6.2 Remove inline `m.Dreamer.MaybeDream(ctx)` call from `FlushMessages` (now handled by Dreamer worker)
- [x] 6.3 Run `make test` — all existing tests pass
- [x] 6.4 Run `make lint` — no new warnings
- [x] 6.5 Verify `make build` produces a working binary (CGO_ENABLED=0)
