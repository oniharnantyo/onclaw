## Why

Slices #1 (`add-agent-memory-core`) and #2 (`add-agent-memory-dreaming`) give onclaw factual and
episodic memory — but facts are flat text. There is no **relational recall**: "which projects use
eino," "what did Alice work on last month," "which tool did we decide to deprecate." Those need
entities and the edges between them, anchored in time. Zep's temporal knowledge graph
(arXiv:2501.13956, Jan 2025) showed this beats flat-vector stores on the Deep Memory Retrieval
benchmark, and Mem0 ships a hybrid vector+graph. onclaw locked "L2 graph in v1" — this slice
delivers it on SQLite, with no graph database and no new extension.

## What Changes

- **Add the L2 knowledge graph** as two tables: `kg_entities` (`name`, `type`, `valid_from`,
  `valid_until`, scoped to agent) and `kg_relations` (`src_entity_id`, `dst_entity_id`,
  `relation`, `valid_from`, `valid_until`). **Temporal validity** is first-class — a changed fact
  is **superseded, not deleted** (`valid_until` set), so the graph remembers what it used to
  believe and when it changed (Zep-style).
- **Add an `EntityExtractor`** (`internal/memory/kg.go`): on each new episodic summary (slice #2's
  `episodic_summaries`), extract entities + relations via a structured LLM call and ingest with
  `valid_from = now`. Extraction runs on slice #2's cheaper **review model**; failures are
  non-fatal (logged, never block the turn).
- **Add dedup**: after each extraction batch, merge semantically-equivalent entities and flag
  ambiguous ones — no duplicate nodes.
- **Add the `kg_search` tool**: graph traversal over entities/relations via a SQLite **recursive
  CTE** (`WITH RECURSIVE`). No graph DB, no CGO extension.
- **Reuse slices #1/#2 substrate** (stores, security scan, review model). Extraction is
  append-mostly; the per-write re-indexing storm of A-MEM-style Zettelkasten is deliberately
  avoided.

## Capabilities

### Modified Capabilities

- `agent-memory`: adds the L2 knowledge graph — entities, relations, temporal validity, episodic-
  triggered extraction, dedup, and recursive-CTE traversal.
- `agent-tools`: adds the `kg_search` tool.

## Impact

- **New code:** `internal/memory/kg.go` (extractor + dedup + traversal), `internal/store/sqlite/
  knowledge_graph.go` (`kg_entities`, `kg_relations`), `internal/agent/tools/kg_search.go`.
- **Modified:** the episodic-write path from slice #2 triggers extraction; slice #1's security scan
  gates ingested entity/relation text.
- **Depends on:** `add-agent-memory-dreaming` (slice #2, for the episodic trigger) and
  `add-agent-memory-core` (slice #1, for the substrate).
- **No data migration** beyond the two new tables.
