# Proposal: wave3-memory-vectors-and-graph

## Why

Wave 3 of `integrate-agent-zero-memory` was deferred pending eval evidence (D11). The evidence gate never fired — the corrected scoreboard reads 100% including multihop, so neither leg was justified by the numbers — but the current fixture (2 multihop questions, 18 turns) is too thin to render a permanent "never" either. This capture proceeds as an explicit **product decision overriding D11**: the paper's three parallel stores (events ✓, notes ✓, associative graph ✗) plus index-before-extract retrieval are the memory-completeness milestone, and the hardened fixture (`harden-memory-eval-multihop`, implemented first) will measure the before/after delta honestly — including the possibility that it shows no improvement.

## What Changes

- **Raw evidence index (index-before-extract)**: raw turn text is embedded asynchronously at turn end — before extraction judgment matters — so a failed extraction never loses a fact; raw hits carry the source event id as their citation.
- **Vector retrieval over extracted stores**: `memory_notes` and `memory_events` rows are embedded at ingestion using the workspace-configured embedding model (provider/model/dimension record + connection test already ship); retrieval fuses lexical and vector channels with Reciprocal Rank Fusion (k=60) per the source paper.
- **Entity-event graph (the paper's associative store)**: tenant-scoped entity labels with bipartite edges to notes and events, resolved in the curation gate's existing extraction side-call (zero extra LLM calls); each edge inherits the narrowest visibility of what it links; traversal is deterministic database hops with zero model calls.
- **Retrieval integration**: `memory.search` gains an entity filter and can return graph-traversed candidates; the intent gate gains an associative routing bucket whose prefetch draws from traversal; the ≤5-candidate prefetch budget and citation-lock discipline are unchanged.
- **No UI in this change** — entities surface through search results and existing provenance surfaces; any entity browser goes through the design-mockup process separately.

## Capabilities

### New Capabilities

- `agent-memory-graph`: the associative store — tenant-scoped entities, narrowest-visibility bipartite edges, extraction-time resolution, identity-bound traversal that cannot leak.

### Modified Capabilities

- `agent-memory-retrieval`: search tool and prefetch become multi-channel (lexical + vector + graph traversal); raw-evidence hits are citable; entity filter added to the search tool.
- `agent-memory-pipeline`: ingestion gains three fail-soft stages — raw-evidence embedding, extracted-row embeddings, entity resolution — off the hot path as today.

## Impact

- New migration(s): pgvector extension, polymorphic `memory_embeddings` table (halfvec, per-row dimension), `memory_entities` + `memory_entity_edges` tables, trigram/tsvector coverage for entity labels.
- `internal/memory/`: worker gains stages (raw embed, row embed, entity resolve — all fail-soft), Searcher gains fusion + traversal, consolidator gains entity hygiene.
- `internal/agents/tools/memory.go`: search tool argument + result shapes extended (backwards-compatible: new fields, one optional filter).
- `internal/agents/runner.go`: prefetch feeds from three channels within the existing budget.
- Depends on `harden-memory-eval-multihop` landing first — its fixture is this change's acceptance instrument; final validation records the associative-category delta.
- Run-cost: one embedding call batch per ingestion job plus raw-turn embeddings; bounded, off the hot path, fail-soft everywhere.
