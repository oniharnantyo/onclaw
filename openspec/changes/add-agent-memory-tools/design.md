## Context

onclaw's searchable memory archive (`memory_documents` + FTS5 + `memory_embeddings` + hybrid
`RankCandidates` scoring) is today **read-only from the agent's perspective**: `memory_search`
reads it, but only the background `ExtractAndFlush` writes it, and `DeleteDocument` is wired to
nothing. The persona template (`internal/agent/templates/AGENTS.md`) tells the agent to "remember
this → `memory/YYYY-MM-DD.md`" — a convention with no backing tool, so the agent hand-writes
unstructured dated files via `write_file`. Separately, embedding setup is only configurable
per-agent (Memory tab) or via bootstrap config; there is no global UI default.

Constraints: pure-Go `modernc.org/sqlite` (no CGO, no `sqlite-vec`); ~2 GB device target;
existing `MemoryStore` interface is the swappable seam tools already depend on; FTS5 triggers
keep the search index in sync on INSERT/UPDATE/DELETE.

## Goals / Non-Goals

**Goals:**
- Give the agent an explicit, secure, searchable CRUD surface on the archive
  (`memory_remember` / `memory_update` / `memory_forget`), reusing `memory_search` for reads.
- Retire the dead `memory/YYYY-MM-DD.md` persona convention in favor of the real tools.
- Make memory always-on and manage its tools uniformly like other tools.
- Add a global, UI-editable embedding configuration default that agents inherit.
- Preserve the swappable `MemoryStore`/`Embedder` seams — no new storage abstraction.

**Non-Goals:**
- Formalizing a new `Embedder` interface type beyond the existing struct.
- Extending the staged-write/`write_approval` flow to archive tools.
- Fixing the latent `embedding_model`-column inconsistency beyond keeping both sides `""`.
- On-device embeddings or an ANN index (precluded by the pure-Go driver / RAM budget).
- A dedicated encrypted embedding secret on the Embeddings page (reuse provider profiles).

## Decisions

### D1 — New tools alongside existing ones (not folded into the `memory` tool)
The existing `memory` tool edits the curated **core file** (`MEMORY.md`) via exact-substring
`target` ops on a single mutable file. Archive operations have a fundamentally different
contract: semantic search, numeric-id addressing, and (now) in-place content mutation. Folding
them into `memory`'s `op`×`store` matrix would go ragged (`replace` is meaningless on the
archive; "forget by description" doesn't fit the exact-`target` model). Separate, clearly-named
tools (`memory_*`) route better for the model. *Alternative considered:* extend `memory` with a
`store` param — rejected for the ragged-matrix reason above.

### D2 — `memory_update` is a real in-place UPDATE
Add `UpdateDocument(ctx, id, content, vector)` to `MemoryStore`. The existing
`memory_documents_au` AFTER UPDATE trigger already re-syncs the FTS5 index, so search stays
correct with no extra plumbing. Re-embed and `INSERT OR REPLACE INTO memory_embeddings` in the
same transaction. Preserves `id` + `created_at`. *Alternatives:* delete+re-insert (loses id,
resets age, breaks any id reference — rejected); supersede/soft-delete with a `valid_until`
column (heaviest, overkill for curated facts — rejected).

### D3 — `update`/`forget` target by numeric `id`; `memory_search` surfaces the id
By-id is deterministic, safe (no accidental mass-action), and mirrors the store contract
(`DeleteDocument(id)`, `UpdateDocument(id,…)`). This requires the coupled change of printing
the document `id` in `memory_search` output so the agent has a handle. *Alternatives:* unique
content-target substring (fragile on long content); natural-language description with
search-then-act (non-deterministic scoring, mass-action risk) — both rejected.

### D4 — Memory is always on; tools managed uniformly (BREAKING)
Per user decision, the `MemoryStore` is constructed unconditionally (never `nil`), and **all**
memory tools are gated only by the standard `ToolRegistryStore` enable/disable + agent tool
whitelist — identical to browser/web tools. The existing per-feature filtering in
`buildTools` (which withheld `memory_search`/`session_search` on `!retrieval_enabled`, etc.)
is removed. The Memory-tab toggles continue to gate their **background** subsystems (extraction,
episodic, dreaming, KG extraction, retrieval auto-injection middleware), not tool availability.
*Trade-off:* this changes the meaning of `retrieval_enabled` (tool-availability → background
auto-injection only) — a deliberate, user-approved breaking change.

### D5 — Model-consistent `EmbeddingModel` resolution across write and search tools
`SearchArchive` filters `d.embedding_model = ?`. When `scope.Embedder != nil`, `memory_remember`, `memory_update`, `memory_search`, and dedup queries all set `EmbeddingModel = scope.Embedder.ModelName`, ensuring remembered documents are findable by subsequent searches and dedup queries under the active embedding model. When no embedder is present (FTS-only mode), `EmbeddingModel = ""` is passed consistently.

### D6 — `memory_remember` writes `Kind="curated"`, `Scope="global"`, `Source="remember"`
Distinguishes agent-explicit memories from background `ExtractAndFlush` rows (`Kind="episodic"`)
without touching the schema. `Scope="global"` matches `memory_search`'s default scope so the
memory is immediately retrievable.

### D7 — Global embedding config in the `preferences` KV table
Store `embedding_provider`, `embedding_model`, `embedding_api_base` as preference rows, exactly
like `default_provider`. The assembly fallback chain becomes
`agent override → global preference → bootstrap memory.embedding_* → chat provider`. *No new
table, no migration.* *Alternative:* a dedicated `embedding_config` table — rejected (YAGNI; KV
suffices).

### D8 — Embeddings page references a provider profile (no new secret plumbing)
The page selects an existing provider profile by name; the API key comes from that profile's
stored secret via `mgr.GetSecret`, identical to today. To use a dedicated embedding key, the
user creates a profile for it on the Providers page. *Alternative:* a dedicated encrypted
embedding secret on this page — deferred (duplicates secret management).

## Risks / Trade-offs

- **[BREAKING] `retrieval_enabled` no longer withholds tools** → Mitigation: the toggle still
  gates background retrieval auto-injection; users who want a tool off use the standard
  Tools-tab disable or agent whitelist. Documented in `docs/memory.md` and the Memory-tab UI.
- **`MemoryStore` interface change breaks all mocks** → Mitigation: add `UpdateDocument` to
  every mock in the same change (enumerated in tasks); CI compile catches any missed.
- **`UpdateDocument` re-embeds on every update (cost/latency)** → Mitigation: acceptable for
  explicit agent edits (low frequency); embedding cache (`embedding_cache`) dedups identical
  text across writes.
- **`embedding_model=""` latent inconsistency remains** → Mitigation: documented as
  out-of-scope; revisit if mixed-model vectors become a real concern.
- **Embeddings-page provider dropdown lists only openai/ollama/cohere** (gemini/google +
  openai-compatible are backend-supported) → Mitigation: settable via config/env/`memory_config`
  JSON; expanding the dropdown is a trivial follow-up.

## Migration Plan

No schema migration required (no new tables/columns; UPDATE uses the existing trigger). Deploy
is a code update. Existing `ExtractAndFlush` "episodic" rows remain searchable
(`EmbeddingModel=""` preserved). Rollback = revert the code change; data written by the new
tools remains valid archive rows readable by the unchanged search path.

## Open Questions

None material — all forks resolved with the user during planning. Minor (deferred): whether to
expand the Embeddings-page provider dropdown to include gemini/google, and whether to later add
a dedicated encrypted embedding secret.
