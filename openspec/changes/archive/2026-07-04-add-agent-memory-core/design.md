## Context

onclaw assembles a real eino ADK agent (`internal/agent/agent.go` `AssembleAgent`) with a handler
chain — `summarization → history → skill → hooks` — and already persists per-conversation history
with summarization (`ConversationStore.SaveSummary`, triggered at 80% of the context window by the
eino summarization middleware's `Callback`). `MEMORY.md` is loaded as one of several persona files
(`LoadPersonaContext`, `internal/agent/context.go`) under a shared `maxPersonaBytes = 16 * 1024`
cap that silently truncates.

The decisive prior art is **convergent**: OpenClaw, Hermes, GoClaw, and eino's own
`adk/middlewares/automemory` (present in onclaw's pinned `v0.10.0-alpha.9` dependency) all describe
the same memory layer — a bounded curated core, a searchable archive, flush-before-compaction, and
a consolidation pass. We build custom (not adopting automemory as a dependency) but borrow its
**shape** (one middleware, `BeforeAgent`/`AfterAgent`) and its **coordination patterns**
(write-cursor dedup, lock/snapshot safety).

Constraints: pure-Go single binary (`CGO_ENABLED=0`), ~2 GB RAM SBC target, security-first
(redaction, DEK/KEK secrets), the store contract/types/impl separation rule, black-box tests ≥ 70%
coverage.

## Goals / Non-Goals

**Goals:**
- Cross-session memory: durable facts that survive a session and resurface next time.
- A bounded curated core that the system curates (no silent truncation).
- A searchable archive (hybrid FTS5 + vector) and searchable past conversations.
- Flush-before-compaction that adds no extra model call per turn.
- Footprint-correct: no CGO, no new SQLite extension, no per-turn background model call.

**Non-Goals:**
- L1 episodic summaries + the dreaming consolidator (slice #2, `add-agent-memory-dreaming`).
- The L2 knowledge graph (slice #3, `add-agent-memory-graph`).
- Local/on-device embeddings (v1 uses remote eino-ext; local is an Open Question).
- A vector DB extension (sqlite-vec/vss) — BLOB + Go cosine is sufficient at onclaw's scale.
- Per-turn dynamic injection — v1 uses frozen-core + on-demand tool (hybrid injection).
- Replacing `MEMORY.md` with a DB — it stays the human-and-machine co-edited core file.

## Decisions

**D1 — One memory middleware, automemory-shaped.** A single `memory_middleware` in the handler
chain owns both directions: `BeforeAgent` injects the frozen curated core; `AfterAgent` runs the
flush. Not a scattered worker pool. *Why over an event-bus + workers:* onclaw's lifecycle already
runs through the eino handler chain; consolidating memory into one middleware matches
`summarization`/`history`/`skill` and needs no new event spine. *Rejected:* a GoClaw-style
event-bus with four standalone workers — duplicates infrastructure the chain already provides.

**D2 — Two L0 layers: curated core + searchable archive.** The curated core (`MEMORY.md`,
~800-token cap, always injected, frozen per session) is distinct from the searchable archive
(`memory_documents`, large, on-demand via `memory_search`). *Why over a single flat store:* the
references all separate "always-in-view curated facts" from "searchable working memory"; a single
store either floods the prompt or hides curated facts. *Rejected:* one SQLite table for everything
— loses the bounded-always-injected property that makes the core useful.

**D3 — Flush-before-compaction on the existing summarization callback.** The summarization
`Callback` in `AssembleAgent` already fires at 80% context with the about-to-be-compacted messages
in hand (`before.Messages` ⊖ `after.Messages`) and an LLM in flight. Memory extraction slots in
*before* `convStore.SaveSummary`, riding that call. Short sessions that never compact are covered
by an `EventStop` flush. *Why:* zero extra model calls per turn — extraction only happens when
compaction is already paying for an LLM call. This is GoClaw's and OpenClaw's proven pattern.
*Rejected:* a standalone every-turn `AfterAgent` extraction pass — a model call on every turn of a
Pi-hosted agent.

**D4 — Hybrid search, vector-heavy 0.3/0.7, no vector extension.** Vectors stored as BLOB columns
(`memory_embeddings.vector`), query embedded via the remote eino-ext embedder, cosine computed in
Go over the candidate set. FTS5 pre-filters candidates, then cosine reranks; final score
`0.3·FTS_norm + 0.7·cosine_norm`. *Why over `pgvector`/sqlite-vec:* onclaw is single-user with
thousands of memories, not millions — brute-force cosine is milliseconds and needs no CGO
extension. *Rejected:* an HNSW/vector extension — CGO or a pure-Go port, unjustified at this
scale.

**D5 — Error-on-overflow, not silent truncation.** A curated-core write that would exceed the cap
returns an error and the agent consolidates in the same turn (Hermes' model). *Why:* the current
silent truncation is the bug; OpenClaw's "truncate the injected copy and signal" is acceptable but
error-on-overflow forces cleanup and keeps the on-disk file honest. *Rejected:* keep silent
truncation — the documented footgun.

**D6 — Frozen curated core at session start + on-demand `memory_search` (hybrid injection).** The
core is injected once at session start and never mutated mid-session (preserves the LLM prefix
cache); deeper recall is the agent's choice via the `memory_search` tool. *Why over per-turn
dynamic injection:* per-turn injection changes the prompt prefix every turn and breaks the cache —
costly on a Pi. This is the OpenClaw/Hermes bet. *Rejected:* automemory-style per-turn topic
selection — precise but cache-hostile.

**D7 — Security scan before injection; write-cursor idempotency.** Memory is injected into the
system prompt, so writes are scanned for prompt-injection / credential-exfiltration / invisible-
Unicode patterns (reusing the `tools.RedactAgenticMessage` seam) before acceptance. Extraction
tracks a write-cursor stashed in `msg.Extra` as `_onclaw_memcursor` (alongside the existing
`_onclaw_seq` / `_onclaw_persisted`), so a retried turn never double-extracts. *Rejected:* trusting
memory content blind — it is attacker-controllable text injected into the prompt.

**D8 — Backend abstraction over SQLite, contract/types/impl separated.** `internal/memory/store.go`
holds the `MemoryStore` / `CoreStore` interfaces; `internal/store/sqlite/memory.go` is the impl;
`types.go` holds DTOs. Same shape as `ConversationStore`. *Why:* the coding-style rule mandates the
split and keeps a future in-memory fake testable with zero coupling.

**D9 — System-curated core via the `memory` tool; optional `write_approval` gate.** The agent
curates `MEMORY.md` through the `memory` tool (`add` / `replace` / `remove` with substring
matching); dreaming (slice #2) will promote into it the same way. An optional `write_approval`
gate stages writes for human review. *Why:* both OpenClaw and Hermes have the system curate the
core — the safeguard is *how* it writes (cap, scan, approval), not whether. *Rejected:* human-only
`MEMORY.md` — contradicts the references and blocks "smarter over time."

**D10 — Remote embeddings via the eino-ext embedding component, acknowledged trade-off.**
Embeddings SHALL be obtained through the eino-ext embedding component
(`github.com/cloudwego/eino-ext/components/embedding/<provider>` — `openai` for the default, plus
`gemini` and `ollama` to cover the providers the initial custom client handled), each implementing
the eino core `components/embedding.Embedder` interface. Embeddings come from the same provider
family onclaw already calls for the LLM (remote), cached by content hash in the SQLite
`embedding_cache` table (the eino-ext `components/embedding/cache` decorator is in-memory and is
not adopted — persistence across restarts is required). *Why over a bespoke client:* eino-ext
already wraps these providers' auth, retries, and response shapes for the model components onclaw
depends on today (`components/model/agenticopenai`, `agenticgemini`, …) — a hand-rolled `net/http`
embedding client duplicates that ACL surface, drifts from upstream provider changes, and is
uncovered by upstream fixes. *Why over local:* onclaw already calls remote models; a local
embedder is a real RAM cost on 2 GB. *Trade-off:* adds the eino-ext embedding sub-dependency
(against the existing eino root, consistent with the model components already pulled in) and
partially compromises the local-first ethos — captured as an Open Question for a future
local-embedding option (the `ollama` eino-ext component is the path of least resistance then).
*Rejected:* a custom `net/http` embedding client — re-implements provider ACLs the eino-ext
components already provide. (This is what shipped in the initial cut; it is to be replaced.)

## Risks / Trade-offs

- **[Remote embeddings cost tokens and need network]** Every archive write + every `memory_search`
  calls the embedder. *Mitigation:* content-hash cache; FTS5-only path when the embedder is
  unreachable; the trivial-message filter skips lookups entirely.
- **[Vector-heavy weights lean on the embedder]** 0.7 vector weight means embedding quality governs
  recall. *Mitigation:* FTS5 carries the 0.3 and the candidate prefilter; weights are config.
- **[Flush rides compaction, which is context-driven]** A long-ish session that never hits 80% only
  flushes at `EventStop`. *Mitigation:* `EventStop` flush is always present.
- **[System-curated `MEMORY.md` can drift]** The agent might curate poorly. *Mitigation:*
  `write_approval` gate; char-limit error-on-overflow; the dreaming review surface (slice #2).
- **[Prefix-cache assumption]** Frozen-core injection assumes the provider honors prefix caching.
  *Mitigation:* onclaw already loads `MEMORY.md` frozen at assembly; no regression.

## Migration Plan

Additive — no data migration. New SQLite tables (`memory_documents`, `memory_embeddings`,
`embedding_cache`) created by migrations in `internal/store/sqlite/db.go`. `MEMORY.md` remains a
file on disk; `LoadPersonaContext` stops truncating it and the memory middleware assumes loading
with an explicit cap. Rollback = revert the change; the new tables are unused by existing code.

## Open Questions

- **Local embeddings:** v1 uses remote eino-ext. A local option (Ollama / sentence-transformers)
  for true offline operation is deferred — revisit if the local-first ethos demands it.
- **Injection granularity:** frozen-core + on-demand tool is v1. If recall precision matters more
  than cache, a per-turn dynamic mode could be added behind config.
- **`MEMORY.md` ownership boundary:** the middleware takes over loading it from `LoadPersonaContext`
  in this slice; the exact split of persona files vs. memory core under the shared budget is to be
  settled in tasks (the core gets its own budget, distinct from the persona-file budget).
