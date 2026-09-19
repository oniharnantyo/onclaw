# Tasks — integrate-agent-zero-memory

## 1. Wave 0 — eval harness

- [x] 1.1 Seed-script: build a LongMemEval-protocol fixture workspace (users, agents, scripted multi-session history with known facts, knowledge updates, temporal and multi-hop questions) driven through the real run path
- [x] 1.2 Scoring harness: recall, citation-validity (claim→opened evidence), scope-safety (cross-member invisibility), and abstention checks; emit a scoreboard report
- [ ] 1.3 Baseline run against current memory (kept docs only) and record the numbers as the pre-change reference in the change folder

## 2. Schema and stores

- [x] 2.1 Migration `000050_memory_stores.up/down`: create `memory_events` and `memory_notes` with `workspace_id`, `visibility` CHECK-constrained tier + `user_id`/`agent_id` owner columns, provenance columns (`origin`, `event_time`, `learned_at`, `source_event_id`), `superseded_by`, `tombstoned_at`, importance, and tsvector + trigram indexes
- [x] 2.2 Domain types and validation: visibility/origin enums, the birth-tuple completeness rule (no write without full provenance), and the visibility-ceiling check (`op.visibility` ≤ source session ceiling) in `internal/domain`
- [x] 2.3 Store ports: `MemoryEvents()` and `MemoryNotes()` sub-interfaces in `internal/store/store.go` (insert, supersede, tombstone, scope-filtered search with filters, list-for-UI, promotion, counts) — sentinel errors per the store contract
- [x] 2.4 Postgres adapter: implement both ports with single-statement writes (append/insert with in-statement guards, mirroring the user_memories append pattern) and structural visibility filtering in every read query
- [x] 2.5 In-memory fake adapter + fake tests (cross-member invisibility, ceiling rejection, supersede/tombstone behavior)

## 3. Ingestion pipeline

- [x] 3.1 New `internal/memory` package: worker with a bounded queue, drained by background goroutines; wired in the composition root (`internal/cli/server.go` → router opts) with granular deps
- [x] 3.2 Enqueue at run end: hook into `RunFinished` in `internal/agents/runner.go` for completed (1422) and failed (2260) statuses; capture turn material + session shape (participant counts → visibility ceiling) at enqueue time
- [x] 3.3 Curation gate: cheap-model call (providers pattern per compact.go) with a prompt receiving turn material + top similar existing notes, emitting ADD/UPDATE/SUPERSEDE/NOOP ops with visibility proposals within the ceiling; default narrow on ambiguity; Langfuse callbacks attached; fail-soft on any error
- [x] 3.4 Explicit-remember detection: gate flags user remember-requests as high-importance/pin-eligible ops
- [x] 3.5 Per-run windowed gister: at run end, gist the session window since the last gist into one `memory_events` row via the participant rule; incremental cursor per session
- [x] 3.6 `memory_ingested` session event: append counts + visibility breakdown (never content) to the session event stream after ops commit; new kind in `internal/agents/events.go`; skipped for scheduled runs
- [x] 3.7 Pipeline tests: fail-soft guarantees (model down, queue overflow), ceiling enforcement, dedupe-before-write, both run statuses ingested

## 4. Retrieval

- [x] 4.1 Intent gate in `runner.go` pre-compose: cheap model, 1.5s hard timeout, fail-open; bucket routing verdict; never blocks or fails a turn
- [x] 4.2 Prefetch injection: on gate hit, scope-filtered top-k (≤5, ≤500 tokens) candidate injection into `composeAgent` context, each candidate carrying `source_event_id` and visibility
- [x] 4.3 `memory.search` tool: register in `internal/agents/tool_registry.go` — read-only, identity-bound via ToolContext, agent-settable filters (time window, visibility bucket, query), hybrid lexical retrieval, structured errors, zero writes
- [x] 4.4 Scope-filtered read queries: shared + own-user + serving-agent rows in every read path (prefetch, search, direct loads); tests asserting cross-member exclusion and wave-3-safe edge inheritance semantics in the query contract

## 5. Web surfaces

- [x] 5.1 API client: memory notes/events endpoints, promotion, tombstone delete, policy switch, morning report in `web/src/lib/api.ts`
- [x] 5.2 Transcript chip: render `memory_ingested` events as a post-turn chip in the chat transcript (counts + visibility breakdown, hydration-safe), with click-through provenance view and per-fact delete
- [x] 5.3 Memory settings pane: `web/src/screens/settings/MemoryPane.tsx` (GatewaysPane pattern + tests) — documents editor (existing), notes/topics browser with provenance links, promotion + delete, conflict flags, extraction-failure counts, and the Memory configuration section (D16): embedding provider/model with a dropdown filtered to embedding-classified catalog models (tri-state resolution per the model-catalog input-modality pattern; unknown gateways fall back to free-text + probe-derived dimension), dimension validation + connection test, visibility default posture (narrow | org-shared), ingestion toggle
- [x] 5.4 Router/handlers: memory UI endpoints in `internal/server/handlers` + `router.go` (membership-scoped reads, permission-scoped writes per the existing memory-endpoint pattern), including workspace memory settings GET/PUT (ToolSettingsStore-pattern structured record: embedding provider config, visibility posture, ingestion toggle)

## 6. Consolidator

- [x] 6.1 Nightly per-workspace pass (worker ticker, ~02:00): merge near-duplicate notes into canonical ones with multi-evidence links, fold into topics; copy-out only; never touches USER.md/WORKSPACE.md
- [x] 6.2 Consolidate-now path: same function triggered on demand from the Memory UI (manual button = one code path with the ticker)
- [x] 6.3 Morning report: conflict flags, merges performed, extraction-failure counts, surfaced in the Memory pane

## 7. Legacy takeout

- [x] 7.1 Migration `000051_drop_daily_memories.up/down`: drop `agent_daily_memories`; release-notes export warning
- [x] 7.2 Memory tool: remove `MEMORY-DD-MM-YYYY.md` / `MEMORY-TODAY.md` paths from `internal/agents/tools/memory.go` (reject with structured error naming the two accepted forms); update tool description
- [x] 7.3 Remove daily-log store methods (port, postgres, fake), daily-log UI remnants, and related tests
- [x] 7.4 Update the `agent-memories` docs/copy: two documents, injection contract, precedence note

## 8. Verification

- [x] 8.1 Full backend suite green (`go build && go vet && go test`) including new fake/postgres tests
- [x] 8.2 Integration tests against Postgres: visibility filtering, ceiling rejection, supersede/tombstone, migration up/down
- [ ] 8.3 Wave-0 harness post-change run: scoreboard vs baseline recorded in the change folder
- [ ] 8.4 Smoke suite: turn produces chip, search tool round-trip, promotion + tombstone via UI endpoints, scheduled-run non-ingestion of chips
