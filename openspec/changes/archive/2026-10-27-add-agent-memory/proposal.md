# Proposal

## Why

Buddies agents forget everything between sessions: a preference the user stated yesterday, a decision reached last week, or a correction the user made an hour ago is gone the moment the session ends, so the user repeats themselves and the agent repeats mistakes. The predecessor project solved this with an expensive extraction pipeline (side model calls, a worker, an entity graph, embeddings) — the 80% that cost the most. The 2026 literature and the two harnesses this app is modeled on (OpenClaw, Hermes Agent) converge on a cheaper shape that fits buddies' existing substrate: memory is plain Markdown in the agent's workspace, the agent curates it during its own turn with the file tools it already has, recall is lexical, and consolidation is deterministic arithmetic over recall signals. This change adopts that shape under one hard constraint: **no part of the memory system makes an additional model call** — no extraction subagent, no summarizer, no embedding service.

## What Changes

- **File-backed agent memory** under each workspace: `memory/MEMORY.md` (curated index), atomic `memory/<fact>.md` notes with a frontmatter schema (name, description, type, concepts, relations including `supersedes` and `contradicts`, wiki-links), and `memory/YYYY-MM-DD.md` daily working notes. Files are the source of truth; the database holds only derived state.
- **Memory block in every turn**: the composed system context gains a budgeted memory block after the operating contract and before the compaction summary — the index, plus the top-N notes whose retention score clears a threshold (silence as the default when nothing qualifies). Fail-soft: a missing or malformed memory directory never fails a turn.
- **In-turn curation conventions**: the block tells the model when and how to save (durable facts, preferences, decisions, corrections), the frontmatter schema, and to supersede rather than contradict — using the existing jailed `write`/`edit` tools. A seeded `memory` skill (global tier, shadowable) carries the read-and-write playbook, mirroring `knowledge-search`.
- **`memory_search` tool**: lexical search over the agent's memory files returning ranked notes, with no model call. Every invocation records a recall trace (query, hits, scores, session origin) — the signal source for consolidation.
- **Background consolidation sweep** (in-process, gateway-aware, zero model calls): ingests memory files since a watermark, dedupes near-duplicates, scores candidates with six recall-trace signals (relevance, frequency, query diversity, recency, multi-day consolidation, concept density) plus a retention decay, promotes gated daily-note lines into the index append-only with source references, archives (never deletes) stale notes, regenerates the managed index section, and records a heartbeat so a dead sweep is visible, not silent.
- **Subagent and non-user origins excluded from curation**: child turns receive no memory block and their recall traces carry their origin; only user-origin traces count toward promotion (the taint gate).
- **Workspace layout and store** gain `memory/` and three derived tables (recall traces, note retention state, sweep state), all removed with the agent.

Out of scope (recorded as follow-up changes, not captured here): **associative recall** — a turn-level index over `session_events`, the concept/edge graph with Hebbian strengthening and random-walk linking, and tiered retrieval escalation (Phase 3); and the **opt-in "dream turn"** — a scheduled LLM maintenance turn triggered by diagnosed shortcomings, default off (Phase 4). No memory UI surface, no settings toggle, and no embeddings ship in this change; each is a deliberate omission, not an oversight.

## Capabilities

### New Capabilities
- `agent-memory`: file-backed, agent-curated memory per workspace; the per-turn memory block and its budget and precision gate; curation conventions and the seeded `memory` skill; the `memory_search` tool and recall traces; the zero-model-call consolidation sweep with its signals, gates, archive, index maintenance, and heartbeat; origin-based exclusion of subagent and unattended turns.

### Modified Capabilities
- `data-store`: the home directory layout gains the workspace `memory/` subtree (index, notes, daily notes, `archive/`), created on demand; the store gains derived memory state rows (recall traces, note retention state, sweep state) that survive restarts and are removed with their agent.
- `agent-tools`: the tool catalog gains the `memory_search` tool.

## Impact

- **Backend (Go)**: new `internal/memory` package (layout, frontmatter notes, lexical index, scoring, gates, promotion, archive, sweep goroutine, skill seeding); `internal/agents/runner.go` `composeSystem` gains the memory block; `internal/agents/compose` registers the `memory_search` tool; a new store migration plus `internal/store` types, interface, and `sqlite/` implementations for the three tables; gateway lifecycle attaches the sweep.
- **Prompts**: the composed system context grows by a bounded block (index budget plus gated notes); the operating contract is unchanged.
- **Frontend**: none in this change (backend-only v1; no bindings regeneration expected).
- **Dependencies**: none new — SQLite via the existing `modernc.org/sqlite` driver, frontmatter parsing with the YAML library already in use, no FTS5, no embeddings.
- **Runtime cost**: zero additional model calls per turn or per sweep; the sweep is bounded arithmetic over the agent's memory files on a fixed interval while the gateway is Running.
- **Compatibility**: the file layout follows the OpenClaw convention (`MEMORY.md` + daily notes), so an agent's memory remains portable to that class of harness.
