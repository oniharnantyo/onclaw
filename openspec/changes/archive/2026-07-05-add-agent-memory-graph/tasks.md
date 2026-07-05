## 1. Graph store (tables + impl)

- [x] 1.1 `internal/memory/kg_types.go` — DTOs: `Entity` (id, agent, name, type, valid_from, valid_until), `Relation` (src, dst, with FromName/ToName for name→ID resolution), `Extraction`, `KGQuery` (with SeedEntityName), `KGHit`
- [x] 1.2 `internal/memory/kg_store.go` — `KGStore` interface (`IngestExtraction`, `DedupAfterExtraction`, `SearchGraph`) — interfaces only
- [x] 1.3 `internal/store/sqlite/knowledge_graph.go` — `kg_entities`, `kg_relations`; `IngestExtraction` (name→ID resolution, creates unknown entities on-the-fly), supersession (set `valid_until`, insert new), `SearchGraph` via `WITH RECURSIVE` (depth-bounded), scoped per agent
- [x] 1.4 `internal/store/sqlite/knowledge_graph_test.go` — black-box: ingest + supersede, traversal finds N-hop neighbors, dedup merges equivalents, search by name, empty graph, scoping, depth bound, path return, invalidated-relation skip

## 2. Extraction + dedup

- [x] 2.1 `internal/memory/entity_extract.go` — `ExtractEntities` + `ExtractEntitiesWithSecurity`: structured LLM call (eino-ext, slice #2's `memory.review.model`) over an episodic summary; emit entities + relations; non-fatal on error (log, skip); security-scanned via `ScanContent`
- [x] 2.2 Dedup pass after each batch — normalize names, merge same-type equivalents, re-point relations; different-type same-name cases left unmerged (future: ambiguous flagging)
- [x] 2.3 Security-scan extracted text via `ScanContent` before ingest (entities/relations are agent-controllable indirect text)
- [x] 2.4 Wire extraction to slice #2's episodic-write path (extract on episodic insert via `MemoryMiddleware.extractAndIngestEntities`)
- [x] 2.5 `entity_extract_test.go` — extraction parses a structured response; non-fatal on malformed/failed model; security threat detection; name normalization; filters incomplete entities/relations

## 3. `kg_search` tool

- [x] 3.1 `internal/agent/tools/kg_search.go` — `kg_search` tool (Category `Memory`): bounded-depth recursive-CTE traversal; returns entity + relation paths; `tools.Register` + auto-seed into `tool_registry`; reads `KGTraversalDepth` from scope config
- [x] 3.2 `kg_search_test.go` — returns connected entities for a seed; respects depth bound; empty graph ⇒ empty result, no error

## 4. Wiring + config

- [x] 4.1 Thread `KGStore` through the assembly path (`agent_session.go`); extraction fires on episodic creation via `MemoryMiddleware`
- [x] 4.2 Config: `memory.kg_enabled` (default on, gates KGStore construction), `memory.kg_traversal_depth` (default 3, fed to `KGTraversalDepth` scope → tool default), extraction uses `memory.review_model` when configured (falls back to primary model)

## 5. End-to-end verification

- [x] 5.1 `make fmt && make vet && make build` — `CGO_ENABLED=0` preserved; no new storage deps
- [x] 5.2 `go test ./internal/memory/... ./internal/store/sqlite/... ./internal/agent/tools/...` — all pass
- [x] 5.3 Automated tests verify all three scenarios: `kg_search` traversal (`TestKGStore_SearchGraph_TraversesFromSeed`), supersession (`TestKGStore_IngestExtraction_SupersedesContradictoryRelation`), dedup merge (`TestKGStore_DedupAfterExtraction_MergesDuplicateEntities`), and different-type non-merge (`TestKGStore_DedupAfterExtraction_DoesNotMergeDifferentTypes`)

## Removed

- `internal/memory/kg.go` — EntityExtractor (dead code, superseded by entity_extract.go). Its 13 tests (kg_test.go) replaced by 20+ tests in entity_extract_test.go with coverage of the live extraction path.
