# Design: wave3-memory-vectors-and-graph

## Context

The memory pipeline (waves 0–2 + three fix changes) is archived with a corrected 100% scoreboard, but that scoreboard is 2 multihop questions deep — too thin to close wave 3 permanently. This capture is the recorded **D11 override**: wave 3 ships as a product milestone (the source paper's full three-store architecture + index-before-extract), with the hardened fixture (`harden-memory-eval-multihop`, lands first) as the honest measuring stick. The spec layer already anticipated this change: `agent-memory-retrieval`'s scope-filtered-reads requirement states graph traversal SHALL inherit edge visibility, and the embedding settings record (provider/model/dimension menu, connection test) shipped with the original change.

Constraints carried forward: every new table is workspace-partitioned; visibility is structural in the query layer; ingestion is off the hot path and fail-soft; provenance tuple at birth; nothing injected that wasn't extracted or human-typed; reads never require a model call.

## Goals / Non-Goals

Goals:
- Vector channel over extracted rows AND raw turns (index-before-extract), fused with lexical via RRF.
- The associative store: entities + bipartite edges, resolved in existing side-calls, traversed in bounded DB hops.
- The hardened fixture measures the associative/recall delta before-vs-after; no regression allowed in existing categories.

Non-Goals:
- No entity-to-entity multi-hop chains in v1 (depth-bounded: entity → rows).
- No UI (entities surface via search results and provenance; an entity browser needs the mockup process first).
- No re-embedding/backfill jobs for dimension migrations (documented degradation: old-dim rows stay lexical-only).
- No change to visibility semantics, promotion, tombstoning, or the chip contract.

## Decisions

- **D1: Vectors live in a polymorphic `memory_embeddings` table, not inline columns.** Shape: (workspace_id, target_type ∈ {note, event, raw}, target_id, dimension, embedding halfvec, visibility snapshot, source_event_id, learned_at). Rationale: one home for three target kinds (including raw turns, which have no row of their own in the memory schema), per-row dimension makes model changes non-migrative, and halfvec halves storage at unchanged retrieval quality for this scale. Alternative considered: pgvector columns on `memory_notes`/`memory_events` (rejected — cannot host raw turns without a third column set, and dimension changes force table rewrites).
- **D2: Fusion = Reciprocal Rank Fusion, k=60, over lexical and vector channels** per the paper; each channel takes its own top-k before fusion; the fused list feeds prefetch and the search tool identically. Rationale: parameter-light, rank-based (no score calibration across channels), trivially testable. Alternatives: weighted linear blend (rejected — requires score normalization across tsvector and cosine scales).
- **D3: Raw-evidence embedding rides the existing worker as a per-job stage** that embeds the turn window's text BEFORE the gister/gate stages run, stamping the participant-rule visibility snapshot. Rationale: index-before-extract is the paper's recoverability guarantee; ordering it first means extraction failures are lossless. Failure = skip the stage, log to the worker counter that the morning report already surfaces.
- **D4: Row embeddings batch into the same ingestion job** — one embedding side-call batch per job (prompt-free, provider-native embeddings endpoint via the resolved side-call lane), dimension from the workspace settings record. Rows with no configured model keep lexical-only forever until a model is set (no silent embedding-model guessing).
- **D5: Stale-dimension policy is exclusion, not re-embedding.** Retrieval filters the vector channel to the workspace's current configured dimension; other dimensions remain lexical-retrievable. Rationale: re-embedding jobs are a cost/complexity cliff with no current need; the degradation is documented behavior, not a bug.
- **D6: Entity resolution rides the gate's existing extraction call** — the op schema gains an optional entities array per op (label, normalized label); the gister gains the same for events. Zero extra side-calls, malformed proposals skip individually (the batch rule already established). Rationale: the gate sees the whole transcript and already classifies; entity labels are extraction-shaped judgment, not a separate concern.
- **D7: Entities are pointers, not content.** No visibility column on `memory_entities` — all gating lives on edges (narrowest endpoint) and linked rows. This is what makes "traversal cannot leak" a structural property: the traversal query walks edges whose inherited tier is in the caller's visible set, then reads linked rows through the existing scope filter. Idempotent resolution per (workspace_id, normalized_label) with normalization = trim + casefold + singular-tolerant hash.
- **D8: Traversal is depth-1 (entity → linked rows), deterministic, zero model calls.** The search tool's `entity` argument resolves via exact normalized label first, then prefix/trigram match on labels; the intent gate's associative bucket routes entity-shaped queries to traversal before fusion. Depth > 1 (entity-to-entity) is explicitly deferred — no current query shape needs it and it doubles the leak surface to reason about.
- **D9: Raw hits and traversed rows cite under the existing citation lock** — the evidence pointer delivered with the result is the citation (raw = source event id; traversed = the linked row's pointer). No new citation machinery.
- **D10: Nightly consolidator gains entity hygiene** — fold normalized-label duplicates (copy-out, supersede, evidence preserved), count merges in the morning report. The consolidator still holds no document-store handle.
- **D11 override recorded:** this change proceeds as a product decision; the hardened fixture's associative category is the acceptance metric — wave-3 passes if associative questions score ≥ the pre-wave-3 hardened baseline with **no regression** in recall/update/temporal/abstention/scope. If the delta is flat, the verdict is recorded honestly and stands as the cost of the completeness bet.
- **D12: Run-cost envelope** — one embeddings batch per ingestion job (n rows + 1 raw text), ~1 intent-gate call per non-trivial turn unchanged, zero read-path model calls. Seeding the hardened fixture post-wave-3 costs roughly the same live-model spend as today; embedding calls are the only addition and are the cheapest lane the provider offers.

## Risks / Trade-offs

- [Graph and vectors add surface area to a pipeline that just reached 100%] → acceptance is regression-gated (D11 override); every new stage is fail-soft and individually skippable, and the chip/report surfaces make silent degradation visible.
- [halfvec at 3072 dims on raw turns could grow storage meaningfully] → raw rows are the only unbounded class; a per-workspace raw-embedding toggle can default on and be turned off under storage pressure (settings field, absence = on).
- [Entity over-merging (same label, different referents — two "Sari" projects)] → v1 accepts the flattening; labels are pointers and worst case a traversal returns extra visible rows, never fewer; disambiguation is deferred until the miss analysis asks for it.
- [Fusion could bury the lexical winner under vector noise] → RRF with per-channel top-k keeps each channel's ceiling; the hardened run's citation_valid category polices this empirically.
- [pgvector extension availability] → migration gates on extension creation with a clear error; Postgres.app and standard clouds both ship it; the store's existing pg_trgm precedent applies.

## Migration Plan

1. Migration: create extension vector; create `memory_embeddings`, `memory_entities`, `memory_entity_edges` with workspace partition indexes; no changes to existing tables.
2. Pipeline stages land behind the existing worker (raw embed → row embed → entity resolve), all fail-soft; with no embedding model configured the stages no-op and the system behaves exactly as today.
3. Retrieval fusion + traversal land in the Searcher; the search tool gains the optional `entity` argument; prefetch gains the associative bucket.
4. `harden-memory-eval-multihop` fixture runs the before/after scoreboard; results recorded in both change folders.
5. Rollback: revert code; tables are additive and inert without the code paths; embeddings/entities rows can be dropped without touching waves 0–2 stores.
