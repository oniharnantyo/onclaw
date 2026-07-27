## Why

The agent's persona template (`internal/agent/templates/AGENTS.md`) instructs it to "remember
this → `memory/YYYY-MM-DD.md`", but **no tool or code backs that instruction** — so the agent
falls back to `write_file` and hand-rolls unstructured, unsearchable dated logs. Meanwhile the
codebase already ships a fully-built searchable memory archive (`memory_documents` + FTS5 +
embeddings + hybrid ranking) that the agent can **read** via `memory_search` but **cannot write
or delete** — only the background `ExtractAndFlush` populates it, and `DeleteDocument` is wired
to nothing. This change closes that loop with an explicit, tool-driven CRUD surface on the
archive, retires the dead dated-file convention, and makes embedding setup a first-class,
globally configurable concern.

## What Changes

- **New agent tools** on the memory archive: `memory_remember` (create), `memory_update`
  (modify in place), `memory_forget` (delete) — reusing the existing `memory_search` (read).
  `memory_update`/`memory_forget` target a memory by the numeric `id` that `memory_search`
  now surfaces.
- **Archive becomes mutable for agent writes:** a new `UpdateDocument` store operation updates
  a document's content in place (FTS re-synced via the existing trigger) and re-embeds it;
  the "documents are immutable" invariant is retired for agent-written documents.
- **Memory is always on:** the `MemoryStore` is constructed unconditionally (never `nil`), and
  **all** memory tools — existing and new — are managed uniformly like other tools (standard
  enable/disable + agent tool whitelist). The per-feature Memory-tab toggles now gate only
  their **background** subsystems (extraction, episodic, dreaming, KG, retrieval
  auto-injection), **not** tool availability. **BREAKING** for the prior behavior where
  disabling `retrieval` withheld `memory_search`/`session_search`.
- **Global Embeddings configuration:** a new **Embeddings** page under the Configuration menu
  sets a global embedding default (provider profile + model + optional base URL) that every
  agent inherits unless it defines its own (existing per-agent Memory-tab override, unchanged).
- **Persona rewrite:** `AGENTS.md` points "remember/update/forget this" at the real tools
  instead of the dead `memory/YYYY-MM-DD.md` convention.
- **Observability:** agent assembly logs the resolved embedding mode
  (`openai/text-embedding-3-small` or `disabled (FTS-only)`) so setup is verifiable.

## Capabilities

### New Capabilities
<!-- None — all changes extend existing capabilities. -->

### Modified Capabilities
- `agent-memory`: the agent gains explicit archive write/update/delete tools;
  `memory_search` returns document ids; archive documents are mutable via `memory_update`;
  memory tools are always available (per-feature toggles gate background subsystems only, not
  tool availability); a global embedding configuration default is introduced that agents
  inherit unless they override.
- `web-ui`: a new Embeddings configuration page under the Configuration menu for the global
  embedding default.

## Impact

- **Code:** `internal/memory/store.go` (+`UpdateDocument` interface), `internal/store/sqlite/
  memory.go` (+impl), `internal/agent/tools/memory.go` (+3 tools, `memory_search` prints id),
  `internal/agent/agent.go` (remove per-toggle memory-tool filtering in `buildTools`),
  `internal/cli/agent_session.go` (unconditional store construction; embedding-mode log;
  global embedding preference in the fallback chain), new `internal/api/handler/embeddings.go`
  + `internal/api/routes.go` routes, `web/src/App.tsx` + new `web/src/pages/EmbeddingsPage.tsx`,
  `internal/agent/templates/AGENTS.md` (rewrite), `docs/memory.md` (retire immutability claim).
- **Interface cascade:** adding `UpdateDocument` to `MemoryStore` breaks every mock
  (`tools/memory_test.go`, `middlewares/memory_middleware_test.go`, `memory/embedding_test.go`,
  `memory/extract_test.go`) until updated.
- **APIs:** new `GET`/`PUT /api/config/embeddings` (preferences KV; behind `requireAuth`).
- **Persistence:** global embedding config stored in the existing `preferences` KV table —
  **no new table, no migration**.
- **Security:** `memory_remember`/`memory_update` scan content via `ScanContent`, identical to
  `ExtractAndFlush` and the curated-core tool. The Embeddings page references an existing
  provider profile (no new secret plumbing).
