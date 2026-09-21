# Tasks: wave3-memory-vectors-and-graph

## 1. Store foundation

- [x] 1.1 Migration: enable pgvector; create `memory_embeddings` (workspace_id, target_type note|event|raw, target_id, dimension, embedding halfvec, visibility, source_event_id, learned_at, workspace partition + (target_type,target_id) + (workspace_id,dimension) indexes)
- [x] 1.2 Migration: create `memory_entities` (id, workspace_id, label, normalized_label, origin, source_event_id, learned_at, created_at; unique (workspace_id, normalized_label)) and `memory_entity_edges` (entity_id, target_type note|event, target_id, visibility, origin, source_event_id, created_at; entity + target indexes)
- [x] 1.3 Store sub-interface + postgres + fake implementations for embeddings and entities/edges (writes batched per ingestion job; reads always workspace-scoped and visibility-filtered)
- [x] 1.4 `go build ./... && go vet ./...` and fake-based store tests green; integration tests against a live Postgres with the extension (skip-tagged)

## 2. Embedding write path (fail-soft stages)

- [x] 2.1 Raw-evidence stage: per ingestion job, embed the turn window text FIRST with the participant-rule visibility snapshot and source event id (index-before-extract ordering); skip-on-failure with worker-counter logging
- [x] 2.2 Row-embedding stage: batch-embed committed notes + events at the workspace's configured dimension; no-model-configured → stage no-ops; per-row dimension stored
- [x] 2.3 Settings plumbing: optional per-workspace raw-embedding toggle (absence = on) on the memory settings record, validated and exposed on GET/PUT
- [x] 2.4 Tests: stage ordering (raw before extract), fail-soft on embedding errors, no-op without model config, mixed-dimension rows coexist

## 3. Fusion retrieval

- [x] 3.1 Searcher: two-channel retrieval (lexical tsvector/trigram + vector cosine at the workspace's current dimension), Reciprocal Rank Fusion k=60 with per-channel top-k, unified result shape carrying provenance
- [x] 3.2 Raw-evidence results: fused results may include raw rows (source turn text, source event id citation) when extracted stores hold no better match; visible-set filtering identical to extracted rows
- [x] 3.3 Wire prefetch and the notes API free-text filter through the same fused path (uniform semantics per the search-tool spec)
- [x] 3.4 Tests: RRF ordering vs hand-computed ranks on a fixed corpus, paraphrase-only query surfaces via vector channel, lexical fallback when no embeddings exist, dimension filtering

## 4. Entity resolution (write path)

- [x] 4.1 Extend the gate's op schema with an optional entities array (label + normalized label per op) parsed in the same side-call; malformed proposals skip individually
- [x] 4.2 Extend the gister to link committed events to mentioned entities; idempotent (workspace_id, normalized_label) resolution; edge visibility = narrowest endpoint tier
- [x] 4.3 Tests: dedupe on repeat labels, edge tier inheritance from user/agent/shared endpoints, malformed-entity skip independence

## 5. Traversal retrieval (read path)

- [x] 5.1 Searcher traversal: seed entity resolution (exact normalized → prefix/trigram on labels) → depth-1 edge expansion → linked rows through the existing scope filter, zero model calls, honoring time-window and limit filters
- [x] 5.2 `memory.search` gains the optional `entity` argument (schema + tool description updated; results carry the same provenance tuple); no identity fields on the wire
- [x] 5.3 Intent gate: associative routing bucket feeds traversal candidates into the shared ≤5 prefetch budget alongside fused channels
- [x] 5.4 Tests: traversal returns only caller-visible rows (Budi-vs-Sari fixture), depth stays at 1, associative prefetch merges within the shared budget, no model call issued during traversal

## 6. Consolidator hygiene

- [x] 6.1 Nightly pass folds normalized-label duplicate entities (copy-out, evidence preserved, merge count in the morning report)
- [x] 6.2 Tests: fold preserves edges and provenance; report counts merges

## 7. Evaluation and acceptance

- [x] 7.1 Land `harden-memory-eval-multihop` first (dependency) and record its pre-wave-3 hardened baseline in this change folder
- [x] 7.2 Run the hardened fixture post-wave-3; record the scoreboard JSON; acceptance per design D11 override: associative ≥ baseline AND no regression in recall/update/temporal/abstention/scope — record the honest delta either way
- [x] 7.3 Manual pass: live turn shows raw-evidence citation, entity-filtered search, and the chip unchanged (counts only); scheduled-run non-ingestion still holds
- [x] 7.4 `go build ./... && go vet ./... && go test ./...` green; `scripts/smoke.sh` passes

## 8. Docs

- [x] 8.1 Update `docs/memory-system.md`: three-store architecture diagram gains the graph + vector/raw channels; settings table gains the raw-embedding toggle; retrieval section documents RRF and traversal; D11 override and scoreboard delta recorded
- [x] 8.2 Update the eval README/help text: fusion + traversal behavior, dimension-exclusion semantics
