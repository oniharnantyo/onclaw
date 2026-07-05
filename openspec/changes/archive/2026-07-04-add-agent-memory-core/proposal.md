## Why

onclaw's agent has no cross-session memory. `MEMORY.md` is loaded every turn by
`LoadPersonaContext` (`internal/agent/context.go`), but it is a static, hand-edited file capped at
`maxPersonaBytes = 16 * 1024` shared across **all** persona files (`IDENTITY.md`, `SOUL.md`,
`AGENTS.md`, …) and **silently truncated** past that cap; nothing writes to it automatically, so
every conversation starts cold and the file quietly eats itself as the persona grows. Episodic
context exists only as per-conversation history (`ConversationStore`) with summarization — nothing
crosses sessions. The agent cannot grow smarter over time.

Reference systems converge on the same answer here — OpenClaw, Hermes, GoClaw, and eino's own
`adk/middlewares/automemory` all ship a memory layer with a bounded curated core, a searchable
archive, and a consolidation pass. onclaw has none of it.

## What Changes

- **Add a memory system** with two L0 layers: a bounded **curated core** (`MEMORY.md`,
  system-curated, ~800-token cap, **error-on-overflow instead of silent truncation**) and a
  searchable **SQLite archive** (`memory_documents` + FTS5 + vector BLOBs).
- **Add a memory middleware** to the agent handler chain (`internal/agent/agent.go`
  `AssembleAgent`): frozen curated-core injection at session start (`BeforeAgent`) plus
  **flush-before-compaction** (`AfterAgent`), riding the existing summarization callback.
- **Flush-before-compaction:** in the existing summarization `Callback` (the one that already calls
  `convStore.SaveSummary`), extract durable facts from the about-to-be-compacted messages
  *before* the summary is persisted — piggybacking the LLM call already in flight, so memory
  extraction adds **no extra model call per turn**. A short session that never reaches the
  compaction threshold is covered by an `EventStop` flush.
- **Hybrid search** over the archive: FTS5 ×0.3 + vector cosine ×0.7, vectors stored as BLOBs with
  cosine computed in Go (no `pgvector`, no CGO), embeddings via a **remote eino-ext embedder**
  cached by content hash. Plus `session_search` — FTS5 over the existing `ConversationStore`.
- **Memory tools:** `memory_search` (archive), `session_search` (past conversations), and `memory`
  (add/replace/remove curated-core entries with substring matching).
- **Safeguards:** strict char limit that errors on overflow; **security scan before injection**
  (prompt-injection / credential / invisible-Unicode patterns), reusing the existing redaction
  seam; **write-cursor in `msg.Extra`** (alongside the existing `_onclaw_seq` / `_onclaw_persisted`)
  so extraction is idempotent across retries.
- **No new `go.mod` storage deps** (modernc SQLite + FTS5 already present); the eino-ext embedding
  component is added against the existing eino dependency. `CGO_ENABLED=0` and the tiny binary
  preserved.

## Capabilities

### New Capabilities

- `agent-memory`: the curated core, the searchable archive, hybrid search, flush-before-compaction,
  frozen-core injection, the security scan, write-cursor idempotency, and the memory tools.

### Modified Capabilities

- `agent-core`: the agent's handler chain SHALL include the memory middleware.
- `conversation-history`: SHALL expose FTS5 search for `session_search`, and the summarization
  path SHALL run the memory flush before persisting the compaction summary.
- `agent-tools`: `memory_search`, `session_search`, and `memory` SHALL auto-seed into
  `tool_registry` (default enabled).

## Impact

- **New code:** `internal/memory/` (`types.go`, `store.go` interfaces, `backend.go`, `middleware.go`,
  `inject.go`, `extract.go`, `search.go`, `embedding.go`, `security.go`, `extractive.go`);
  `internal/store/sqlite/memory.go` (`memory_documents`, `memory_embeddings`, `embedding_cache`);
  `internal/agent/middlewares/memory_middleware.go`; `internal/agent/tools/memory.go`.
- **Modified:** `internal/agent/agent.go` (handler chain + summarization-callback flush hook);
  `internal/agent/context.go` (`LoadPersonaContext` stops owning/silently-truncating `MEMORY.md` —
  the memory middleware loads it with an explicit cap and truncation notice); store wiring in the
  CLI assembly root.
- **No data migration:** additive — new tables via migrations; `MEMORY.md` stays a file on disk.
- **Slices:** this is slice #1 of three (`add-agent-memory-core`). Slice #2 (`add-agent-memory-dreaming`)
  adds L1 episodic + the dreaming consolidator; slice #3 (`add-agent-memory-graph`) adds the L2
  knowledge graph. Each ships usable value on its own.
