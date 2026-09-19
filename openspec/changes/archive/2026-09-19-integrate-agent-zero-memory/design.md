# Design — integrate-agent-zero-memory

## Context

OnClaw's memory today is three append-only documents (USER.md, WORKSPACE.md, daily logs) with an always-inject composer seam (`composeAgent`, runner.go:2166) and a single-statement atomic append with in-statement cap guard (`internal/store/postgres/memory.go`). Nothing extracts, indexes, retrieves, or re-reads; the daily log is never injected at all. The Agent Zero Memory paper (arXiv 2608.29606) supplies the target architecture — background distillation into parallel episodic/semantic stores, provenance at birth, citation-locked retrieval — and onclaw already ships the substrate: `session_events` (append-only raw evidence with stable IDs), Postgres, the `RunFinished` hook chain (runner.go:1422/2260), the tool-registry pattern (tool_registry.go:175), and a side-call model pattern (compact.go). This design maps the paper onto that substrate; motivation and scope live in proposal.md, behavior contracts in the specs.

## Goals / Non-Goals

**Goals**: memory that is (1) always available without retrieval for general user/workspace facts (kept docs), (2) searchable, cited, and scope-safe for everything else (new stores), (3) ingested entirely off the hot path, (4) measurable (wave-0 harness before capability waves).

**Non-Goals**: pgvector/embeddings and the entity–event graph (wave 3, follow-up change); per-message ingestion (rejected); making the consolidator an agent run; auto-promoting machine-extracted facts into the kept documents; migrating daily-log content.

## Decisions

**D1 — Two layers, not one.** USER.md/WORKSPACE.md stay exactly as they are (schema, tool, endpoints, composer injection) as the manually curated, always-injected tier. The paper's stores are added alongside, not merged: a blob-document and a fact-store answer different needs (rent-always general context vs pay-on-use cited depth). *Alternative considered:* converge everything into the notes store (rejected — loses the zero-retrieval guarantee and the humans' simple editing surface).

**D2 — Ingestion trigger: turn end, both run statuses.** The worker enqueues at `RunFinished` (completed at runner.go:1422, failed at :2260 — a failed run still contains a real user turn; the gate filters noise). Per-message is rejected (the paper's own critique of Mem0/Zep write-path cascades); "session end" does not exist in onclaw (sessions are long-lived threads); nightly/lazy defer freshness for no architectural gain. *Constraint honored:* distillation off the hot path, distill-once, raw stays intact on failure.

**D3 — Gister unit: per-run windowed gist.** Each run end gists "everything in the session since the last gist" into one `memory_events` row. Incremental windows, no reprocessing. *Alternative:* idle-based session windows (needs a sweeper; fewer, staler events).

**D4 — Two-axis scope.** `workspace_id` is the tenant partition on every row (onclaw's existing "no query without workspace scope" invariant); `visibility` ∈ {shared, user, agent} with nullable `user_id`/`agent_id` owner columns and CHECK constraints is the within-tenant tier. Stamped at birth: events by the participant rule (no human → agent; one human → user; ≥2 humans → shared); notes by the gate's "who is this true for?" within a **ceiling inherited from session shape** (a DM can birth at most user-visibility facts — enforced in op validation, not prompts); default narrow on ambiguity (asymmetric cost: promotion is cheap, a leak is not). Only humans widen (promotion, audited); nothing narrows silently. A per-workspace policy switch (`narrow` | `org-shared`) changes the stamped defaults without schema change — `org-shared` reproduces the paper's posture for single-team self-hosters.

**D5 — Provenance at birth.** `(origin ∈ {manual, dialogue, infer, doc}, event_time, learned_at, source_event_id)` on every events/notes row, validated on INSERT — no backfill path exists. This is simultaneously the citation target for retrieval and the injection-defense audit trail (a transcript claiming things learns `dialogue`/`infer` provenance, never `manual`).

**D6 — Non-destructive updates, tombstone deletes.** Corrections are new rows with `supersedes` pointers; deletion is a provenance-recorded tombstone hidden from all read paths. History stays answerable ("what changed") alongside current state.

**D7 — Doc-over-notes precedence.** The gate never writes to USER.md/WORKSPACE.md and never supersedes human-curated content; contradictions become review flags in the morning report. *Alternative:* let the pipeline compile into the docs (rejected — reintroduces per-turn coherence machinery; the docs' freshness comes from write-through self-edits and human edits instead).

**D8 — Read path: fail-open gate + read-only search + citation lock.** The intent gate runs pre-compose (cheap model, ≤1.5s hard timeout, fail-open to "self-contained"); on hit it prefetches ≤5 candidates ≤500 tokens, each carrying `source_event_id`. `memory.search` is the only memory tool the model gets (read-only, identity-bound via ToolContext, hybrid tsvector+trigram with agent-settable filters, zero model calls inside). Citation lock: only evidence opened this turn is citable; empty result ⇒ the agent says nothing is recorded. Visibility filtering is structural in the query layer (shared + own-user + serving-agent rows) — never prompt-policed. No model-facing ingest tool exists: explicit "remember this" is honored by gate detection (high-importance, pin-eligible), not by tool call. *Alternative:* agent self-edit write tools (the earlier hybrid; dropped with the takeout — the gate sees the whole transcript and out-extracts any self-report, and write-path tools are the paper's criticized failure axis).

**D9 — Lexical-first indexing; vectors deferred.** tsvector + trigram in the wave-2 migration; pgvector + the embedding-provider decision move to wave 3, gated on wave-0 numbers. Lexical-only is fully functional (the stores, filters, citation lock, and scope model are vector-independent).

**D10 — Side-calls: providers pattern, Langfuse-traced, fail-soft.** The gate, gister, and consolidator build models the way compact.go does (providers registry; cheap-tier default), run as background jobs in a new `internal/memory` package, and emit traces through the in-flight Langfuse callbacks. Any failure skips the stage; raw session events remain for reprocessing; runs are never touched.

**D11 — Chip via session event.** The gate's committed ops append a `memory_ingested` session event (counts + visibility breakdown, never content) rendered as a post-turn chip in ChatRoute with click-through to provenance and per-fact tombstone delete. Channels disclose private-extraction counts without content. Hydration is free — it is just another session event.

**D12 — Consolidator: nightly batch per workspace + morning report.** Merge near-duplicate notes into canonical ones (multi-evidence links), fold into topics, report conflict flags and extraction-failure counts. Copy-out only; documents untouched. *Alternative:* consolidation as agent runs or cron schedules (rejected — it is infrastructure, not an agent execution).

**D13 — eino automemory middleware: crib, don't adopt.** Its file-per-agent model cannot express tenant/visibility scoping, citation-locked retrieval, or the management UI. Its pipeline shape (topic-selection, extraction agent, dream consolidation) is the reference for the gate/gister/consolidator prompts.

**D14 — Daily logs dropped without migration.** Table `agent_daily_memories`, the `MEMORY-*` tool paths, and related UI are removed. Release notes warn operators to export; the pipeline re-derives durable facts from session history going forward.

**D15 — Eval harness before capability waves.** Wave 0 seeds a workspace with LongMemEval-protocol scenarios and scores recall/citation/scope behavior, giving waves 1–3 a scoreboard and gating wave 3's vector investment.

**Parameter pins**: prefetch ≤5 candidates / ≤500 tokens; gate timeout 1.5s; document cap unchanged (32,000 chars, existing `MaxMemoryContentChars`); consolidation cadence nightly 02:00 + consolidate-now on demand; gate/gister/consolidator on the cheapest configured model tier.

**D16 — Embedding models are workspace-chosen on the Memory settings page; dimensions via a supported menu.** The embedding model is configured on the Memory settings page — NOT the generic providers page — so all memory behavior is cohesive in one surface. Storage follows the workspace-scoped structured-settings pattern (ToolSettingsStore-style: a settings row for memory, absence = defaults). The Memory configuration section hosts, at minimum: the embedding provider (endpoint, API key, model — with a dimension check against the menu) and the visibility default posture (narrow | org-shared); natural future residents are an ingestion on/off toggle and consolidator schedule tuning. Because pgvector column dimensions are fixed at DDL while the choice is per workspace, wave 3 creates a fixed menu of dimension columns per table — `embedding_384`/`embedding_512`/`embedding_768`/`embedding_1024`/`embedding_1536`/`embedding_3072` (halfvec, one HNSW index each) — covering every common model family including the sub-700 small/self-host tier (MiniLM 384, bge-small 384, Cohere light 384, voyage-3-lite 512); Matryoshka models (OpenAI 3-*, nomic, jina v3, voyage-3-large, Cohere v4) may be requested at a menu-supported dimension instead of being rejected, and the settings save path validates the chosen model's dimension against the menu (connection test embeds a probe string). The model dropdown lists only embedding-classified models, resolved the way the model-catalog's input-modality requirement resolves modalities: provider-scoped, tri-state (embedding / not-embedding / unknown) from the catalog's per-model metadata; unknown — openai-compatible gateways and unmapped providers — falls back to free-text entry where the save-time probe both classifies the endpoint and discovers its dimension. Each row carries `embedding_model`; switching a workspace's model triggers an incremental background re-embed; queries embed with the workspace's configured model and hit the matching column, workspace-filtered. *Alternative considered:* hosting embedding config on the generic providers page (rejected — disconnects it from the memory surface it governs) and normalizing all models to one storage dimension via zero-padding (preserves cosine exactly — rejected as storage-wasteful and conceptually muddy). Rejected outright: a single hardcoded dimension (breaks self-hosted models) and per-workspace tables (query fan-out).

## Risks / Trade-offs

- [Gate misroutes a context-needing turn as self-contained] → the search tool is the structural backstop; wave-0 harness measures miss rate before we consider a smarter (never pricier) gate.
- [Lexical-only recall misses paraphrase-heavy questions until wave 3] → accepted, measured by the harness; tsvector+trigram covers most workspace-entity queries.
- [Privacy leak via mis-stamped visibility] → ceilings enforced in op validation (not prompts), CHECK-constrained columns, structural query filters, and tests asserting cross-member invisibility.
- [Extraction cost scales with traffic] → cheap models, NOOP-heavy gating, dedupe-before-write, nightly batch for the rest; per-workspace ingestion counters visible in the morning report.
- [Daily-log data loss on upgrade] → export warning in release notes; table restorable via down-migration (empty) within the release window.
- [Two memory layers confuse users] → the Memory UI presents them as one page: documents (edited) + facts (derived, deletable), with the precedence rule stated in copy.

## Migration Plan

1. Ship `000050` (new tables `memory_events`, `memory_notes`, indexes, visibility columns) — additive, safe.
2. Ship the worker + retrieval behind the existing tool allowlisting; chips appear as turns complete.
3. Ship `000051` dropping `agent_daily_memories` and the legacy tool paths in the same release as the takeout; release notes carry the export warning. Rollback: down-migrations restore an empty daily table; the new stores are additive and can coexist with the legacy tool if a rollback is needed.

## Open Questions

- Gateway chats (telegram-gateway change): does an external gateway identity map to a member (`user` ceiling) or stay unmapped (`agent` ceiling)? Cross-change dependency — this change consumes the mapping but does not define it.
- Embedding provider story (wave 3): resolved in direction by D16 — workspace-chosen in settings, menu-of-dimensions columns. Remaining sub-decision: the exact provider-kind config shape (new provider type fields: endpoint URL, API key, model id, dimension) — settled when wave 3 starts; lexical-only ships without it. Schema rule: the dimension is dictated by the chosen model (never chosen independently — switching models invalidates every vector and forces re-embed), hence the per-row `embedding_model` column. pgvector nuance: dimensions >2,000 exceed the HNSW index limit of plain `vector` — the menu uses `halfvec` throughout.
- Dedupe similarity tuning (text-overlap threshold) and gist window sizing: tunables, defaulted conservatively, adjustable after wave-0 data.
