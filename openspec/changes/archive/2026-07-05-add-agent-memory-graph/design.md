## Context

Slices #1 and #2 give onclaw a curated core, a searchable archive, episodic summaries, and a
dreaming consolidator. Facts are stored and recalled as flat text. This slice adds the relational
layer — entities and the typed, time-anchored edges between them — so the agent can answer
relational questions that keyword/vector recall over flat text answers poorly.

Zep (arXiv:2501.13956) introduces a **temporal knowledge graph**: facts carry validity windows
(when they became true, when they stopped being true), which both improves retrieval and lets the
agent reason about change. Mem0 pairs a vector store with a graph and consolidates conflicts
rather than duplicating. We borrow both ideas and put them on onclaw's existing SQLite substrate.

Constraints unchanged: pure-Go single binary, ~2 GB RAM SBC, no graph database, no CGO extension,
black-box tests ≥ 70%.

## Goals / Non-Goals

**Goals:**
- Entities + typed relations with temporal validity (supersession, not deletion).
- Episodic-triggered extraction on the cheaper review model, non-fatal.
- Relational search via recursive CTE — "what is connected to X."
- Dedup so the graph doesn't fill with duplicate nodes.

**Non-Goals:**
- A graph database or a vector+graph hybrid engine (Zep/Mem0 scale beyond onclaw's needs).
- Per-write Zettelkasten re-indexing (A-MEM) — explicitly avoided for footprint.
- Cross-agent graph merging (onclaw is single-user; per-agent scoping is enough).
- Changing the curated-core or archive contracts from slices #1/#2.

## Decisions

**D1 — Graph as two SQLite tables, traversal by recursive CTE.** `kg_entities` and `kg_relations`
are ordinary tables; `kg_search` traverses with `WITH RECURSIVE`. *Why over a graph DB / graph
extension:* onclaw's graph is small (thousands of nodes for one user) and SQLite already does
recursive CTEs natively in modernc — zero new dependencies, no CGO. *Rejected:* an embedded graph
DB or a graph extension — unjustified footprint and a new dependency surface.

**D2 — Temporal validity: supersession, not deletion.** Every entity/relation carries `valid_from`
and a nullable `valid_until`. A changed fact sets `valid_until = now` on the old row and inserts a
new `valid_from = now` row; retrieval skips `valid_until IS NOT NULL` rows unless a historical
query asks for them. *Why:* Zep's result — the graph remembers what it used to believe and when it
changed, which matters for "why did we decide X" recall and for trust. *Rejected:* in-place update
— destroys history; deletion — irreversible and loses audit.

**D3 — Extraction on episodic creation, structured LLM, non-fatal.** When slice #2 writes an
`episodic_summaries` row, the extractor pulls entities + relations from the summary text via a
structured (tool/JSON) LLM call and ingests with `valid_from = now`. It runs on slice #2's
`memory.review.model`. Extraction errors are logged and skipped — they never break a turn or the
consolidation pipeline. *Why:* episodic summaries are the right granularity (not raw turns);
non-fatal matches GoClaw/OpenClaw. *Rejected:* extract from every turn — too many calls for a Pi;
extract from raw messages — noisier than from summaries.

**D4 — Dedup after each batch.** After ingesting an extraction batch, a Go pass merges
semantically-equivalent entities (same type + normalized name) and flags ambiguous ones for the
review surface. Relations re-point to the surviving entity. *Why:* without dedup the graph fills
with "Alice"/"alice"/"A. Lee" duplicates and traversal fragments. *Rejected:* no dedup — retrieval
quality collapses within weeks.

**D5 — `kg_search` scopes per agent; `memory_search` and `kg_search` complement.** `memory_search`
(slice #1) retrieves factual text chunks; `kg_search` traverses relations. They are separate tools
the agent chooses between, mirroring OpenClaw/GoClaw. *Why:* the two recall modes answer different
questions; fusing them hides the choice from the agent. *Rejected:* one merged search — loses the
clean text-vs-relations distinction.

**D6 — No per-write re-indexing.** Extraction is append-mostly per episodic summary; we do **not**
re-organize or re-link the whole graph on each insert (the A-MEM Zettelkasten approach). *Why:*
that re-indexing is a per-write cost that scales poorly on a Pi; supersession + dedup give the
consolidation benefit without it. *Rejected:* A-MEM-style dynamic re-linking — footprint trap.

## Risks / Trade-offs

- **[Extraction quality governs graph quality]** A weak extractor produces junk nodes/edges.
  *Mitigation:* structured prompts; dedup; the review surface from slice #2 surfaces flagged
  ambiguities; extraction is non-fatal so bad batches don't compound.
- **[Graph grows over a long life]** Supersession keeps history, so rows accumulate. *Mitigation:*
  a future compaction can collapse long-superseded chains; v1 keeps it simple (storage is cheap,
  the set is single-user).
- **[Recursive CTE cost on very wide graphs]** Deep traversals could be slow. *Mitigation:* bound
  traversal depth in `kg_search`; the single-user graph stays small.

## Migration Plan

Additive — two new tables (`kg_entities`, `kg_relations`) via migration. Requires slices #1 and #2
landed. Rollback = revert; the tables are unused by earlier slices.

## Open Questions

- **Extractor prompt/shape:** structured tool-call vs JSON — finalize in tasks (lean structured
  tool-call for provider portability via eino-ext).
- **Traversal depth default:** bound at 2–3 hops; tune once real graphs exist.
- **Historical queries:** whether `kg_search` exposes an "as-of" mode in v1 or defers it.
