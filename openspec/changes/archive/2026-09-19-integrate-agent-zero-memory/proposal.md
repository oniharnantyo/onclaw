# Integrate Agent Zero Memory

## Why

OnClaw's agent memory today is a diary nobody rereads: three append-only documents (USER.md, WORKSPACE.md, daily logs) with no extraction, no retrieval, no provenance — the daily log is never injected and grows until the 32k-cap 422 blocks it. The Agent Zero Memory architecture (arXiv 2608.29606) — parallel episodic/semantic memories with provenance stamping, citation-locked agentic retrieval, and background distillation off the conversational hot path — is the blueprint for closing that gap. OnClaw already ships the hard parts of the substrate: `session_events` is a clean raw-evidence layer, Postgres is the store, `composeAgent` is the per-turn injection seam, and the `RunFinished` hook chain (runner.go:1422) is the trigger point.

## What Changes

- **Keep** USER.md and WORKSPACE.md as the always-injected general memory documents — current save mechanism unchanged (memory tool read/append + editor PUT + `composeAgent` injection). These are the manually curated tier; they outrank extracted notes on conflict.
- **BREAKING: drop the daily-log scope** — `agent_daily_memories` table, `MEMORY-DD-MM-YYYY.md` / `MEMORY-TODAY.md` tool paths, and related UI. Replaced by the ingestion pipeline and `session_events` (the raw archive).
- **Add background ingestion** (`internal/memory` worker, off the hot path): turn-end curation gate (extract atomic facts → dedupe against existing notes → ADD/UPDATE/SUPERSEDE/NOOP ops) and a per-run windowed gister (onclaw sessions are long-lived threads; the gist unit is "everything since the last gist"). Both completed and failed runs are ingested.
- **Add two new stores** with the two-axis scope model: `workspace_id` (tenant partition, every row) + `visibility` ∈ {shared, user, agent} with owner columns, stamped at birth alongside provenance (origin, event_time, learned_at, source_event_id). Visibility ceilings inherit from session shape; only humans widen.
- **Add retrieval**: intent gate before compose (cheap model, 1.5s timeout, fail-open) decides prefetch; `memory.search` tool (read-only, identity-bound) for agent-initiated retrieval over hybrid lexical indexes (tsvector + trigram); citation lock — only evidence actually opened is citable; empty result = abstain.
- **Add the `memory_ingested` session event** rendering a post-turn chip in chats/channels with click-through to provenance, visibility, and tombstone-delete.
- **Add Memory settings UI**: notes browser, promotion (visibility widening), tombstone delete, per-workspace visibility policy switch (narrow default | org-shared), morning report (conflict flags, extraction failures).
- **Add a nightly consolidator** (per workspace): merge near-duplicate notes, fold into topics, refresh nothing in the kept .md docs, emit the morning report.
- **Wave 0 eval harness first**: LongMemEval-protocol seeded-workspace scoreboard, so every later wave is measured.
- **Deferred to wave 3 (follow-up change)**: pgvector embeddings + embedding-provider story, entity–event graph stores.

## Capabilities

### New Capabilities
- `agent-memory-pipeline`: the ingestion build pipeline — turn-end trigger (completed and failed runs), curation gate ops with dedupe-before-write, per-run windowed gister, provenance + visibility stamping at birth (ceilings inherited from session shape), nightly consolidator, and the `memory_ingested` chip event.
- `agent-memory-retrieval`: the read path — intent gate prefetch (fail-open), `memory.search` tool, hybrid lexical retrieval with agent-set filters, citation lock and abstention, scope-filtered visibility at read, and the kept cores' injection contract.

### Modified Capabilities
- `agent-memories`: daily-log scope removed (**BREAKING**); USER.md / WORKSPACE.md remain the general-memory documents with their current save mechanism; doc-vs-notes precedence (docs win; gate conflicts become review flags, never overwrites).

## Impact

- **Backend**: new `internal/memory` package (worker, gate, gister, consolidator, search); `internal/agents/runner.go` (enqueue at RunFinished, intent gate pre-compose); `internal/agents/events.go` (new event kind); `internal/agents/tool_registry.go` (`memory.search` registration); `internal/store` + postgres + fake (two new sub-interfaces); `migrations/000050+` (new tables, lexical indexes, daily-log removal); `internal/server/handlers` + `router.go` (memory UI endpoints).
- **Frontend**: `web/src/screens/settings/MemoryPane.tsx` (GatewaysPane pattern), `ChatRoute.tsx` transcript chip, `web/src/lib/api.ts`.
- **Side-calls**: cheap models via the existing providers pattern (compact.go precedent); every side-call Langfuse-traced via the in-flight tracing change.
- **Cross-change dependency**: gateway chats' identity mapping (telegram-gateway change) determines gateway-session visibility ceilings (`user` if mapped to a member, `agent` if unmapped).
- **Removal**: `agent_daily_memories` data is not migrated; operators are warned in release notes to export before upgrading.
