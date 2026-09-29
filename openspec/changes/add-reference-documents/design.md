# Design

## Context

The substrate for this change already ships: the attachment storage port (local + S3 drivers, capability URLs, per-blob backend), the pure-Go document converter registry behind `document.read`, the `document.*` family seam (catalog, permission key, tool card, hook target), compose-time metadata injection (skills pattern), the subagent tool with fresh child sessions, and the right panel's file-source preview. Chat attachments remain the per-turn lane and are untouched. The design reuses these seams; nothing here introduces a new retrieval stack — the section index is Postgres FTS (`tsvector` + GIN), and ingest makes zero model calls.

Evidence base (explore record): DCI / direct corpus interaction (arXiv 2605.05242 — agent + terminal tools beats retriever pipelines, no index at all), ReadAgent (2402.09727 — the lookup loop, not the gist layer, carries the gains: 87.2% vs 77.5% gist-only), PDFTriage (2309.08872 — structure-addressable access over raw documents beats flattening), BEIR (2104.08663) plus the biomedical RAG study (2505.07917) — lexical retrieval suffices out-of-domain, and the parser comparative study (2410.09871) — born-digital documents need no ML parsing. Upgrade paths that preserve this design's storage layout: RARG-style relevance steering (2607.24223), Dr-DCI workspace steering (2606.14885), ReadAgent per-document gisting (one LLM call at upload), and vector fusion via the existing memory-embedding RRF lane if vocabulary mismatch ever shows up in transcripts.

## Goals / Non-Goals

**Goals:**
- One artifact per document: the original bytes. Everything else derived, rebuildable, invisible.
- Retrieval as agent tools (`document.search`, scoped `document.read`) — never hidden context-stuffing middleware.
- Locator-accurate citations (page/slide/sheet/heading) verifiable against the real file.
- Visibility (attached agents/channels, admin promotion) enforced identically at compose and query time.
- Ingest cost: deterministic CPU only. Zero embeddings, zero LLM calls, no per-page anything.

**Non-Goals:**
- Vector/embedding retrieval; per-document LLM gisting; RAG reranking.
- Chat-drop upload flow (popover stub only; settings-pane upload is v1).
- Legacy `.doc`/`.ppt` acceptance; OCR for scanned PDFs (page-level reads only, surfaced at upload).
- Per-agent document attach beyond the visibility tiers (no per-agent overrides beyond the attach lists).
- Renaming or moving the existing `document.read`/`document.create` contracts beyond the scoped-read delta.

## Decisions

- **D1 — Original file is the only artifact; text is discarded after indexing.** Upload → store bytes via the storage port under a new `reference` blob kind → convert in-memory → section → index → drop text. Rationale: no second copy to drift; re-upload replaces one blob and rebuilds. Alternative rejected: a stored `.md` sibling (greppable, but duplicates content and goes stale on replace).
- **D2 — Sectioning operates on converted markdown, one format-agnostic sectioner.** Converters already emit markdown headings; pptx/xlsx converters emit slide/sheet markers. The sectioner keys on markdown structure, not native formats. txt/csv bypass conversion entirely (single section). New per-format knowledge stays inside converters — the only converter change is the PDF emitting page-break anchors (it already walks pages internally).
- **D3 — Locators generalize page numbers.** `document_sections.locator` is a rendered string (`p. 30`, `slide 5`, `sheet Users`, `§Webhooks → Signing`, empty for txt/csv) alongside `locator_kind`, so search hits and citation chips render uniformly. Citations stay verifiable against the real artifact for every type.
- **D4 — `document.search` rides the family seam.** Registered like `document.read`/`document.create`: catalog entry, permission key under the workspace tool gate, tool card rule, hook target — no new plumbing. Input `{query}`; output bounded hits `(document, heading, locator, snippet)`.
- **D5 — Scoped reads extend `document.read` with two optional params.** `pages` (PDF only — the converter's page anchors make page extraction a slice, not a re-parse) and `section` (title match on headings/slide/sheet markers; unknown → structured not-found). Both bounded by the existing output cap. Alternatives rejected: one unified `locator` string param (ambiguous across types), a separate `document.read_section` verb (family bloat for a parameter's worth of behavior).
- **D6 — Manifest injection composes like skills metadata.** A compose step emits one block: `name — description — count — top-level TOC` per visible document. Budget-capped; overflow collapses TOC (never drops the entry) so discovery survives large libraries. Injection skipped when the agent's tool gate excludes document tools. Location mirrors the skills slot ordering in compose.
- **D7 — Visibility is one predicate, enforced twice.** `visible(run, doc) = doc.promoted ∨ run.agent ∈ doc.attached_agents ∨ (run.session is channel ∧ run.channel ∈ doc.attached_channels)`. Evaluated at compose (manifest) and at query time (search WHERE-clause + mount composition). The references mount mounts only visible docs, making path-level visibility a jail property rather than a runtime check. Scheduled runs resolve by agent bindings only (delivery target never widens scope). Schema: `reference_documents.scope` (`attached` | `workspace`), join tables `reference_document_agents`, `reference_document_channels` (the enabled_mcps join pattern). Promotion/demotion = scope flip, admin-gated via a new permission-catalog entry; upload/attach requires membership + configurability of the targets.
- **D8 — The references mount is read-only and jailed.** Mounted at `references/` beside the agent tree; `ls`/`glob` work (name discovery), `grep` is a natural no-op on binary types and works on md/txt (native DCI lane), write/delete tools reject the subtree. Name collisions on mount suffix deterministically.
- **D9 — Subagents inherit by construction; one test pins it.** Child sessions clone the parent's tool registry and compose pipeline, so the manifest and document tools arrive automatically. The inheritance test (spawned session resolves `document.search` + recites the manifest) is the load-bearing regression guard. No routing middleware, no librarian agent type.
- **D10 — Frontend reuses three existing patterns.** Documents pane follows the skills-gallery pattern (single edit surface); agent/channel sections are read-only chip lists with scope badges (connections-section pattern); chat citation chips are markdown capability-URL links (existing local-file-link→right-panel behavior). Composer popover resolves the conversation's visibility predicate; mention insertion reuses the drop-lane pointer-note machinery. Right panel gains a Documents source beside Files/Browser.

## Risks / Trade-offs

- [Lexical retrieval misses paraphrases with no shared vocabulary] → Mitigated by manifest descriptions (human-written vocabulary bridge), the agent's multi-hop refinement, and grep over md/txt; recorded upgrade path is RARG-style steering or vector fusion via the existing RRF lane — neither changes the storage layout.
- [PDF page anchors inaccurate on exotic layouts] → Outline-derived TOCs preferred; page-level fallback keeps search+scoped-read functional; wrong-anchor cost is one extra `document.read` turn, surfaced in transcripts.
- [Large libraries bloat the manifest] → Budget cap with TOC collapse (D6); entries never dropped, so discovery degrades gracefully; absolute counts stay small in real workspaces.
- [FTS index drift vs blob after failed re-upload] → Replace is transactional: blob swap + rebuild in one step, old sections deleted before new insert; rebuild-from-blob is the recovery path and is tested.
- [Sectioner mis-splits heading-less documents] → txt/csv degrade to a single section (whole-doc search still works); heading detection is converter-owned markdown structure, not heuristics on raw bytes.
- [Query-time FTS adds Postgres load on chat hot paths] → GIN-indexed single-table lookups bounded by LIMIT; sections tables are workspace-scoped and tiny relative to existing session-event tables.

## Migration Plan

New migration pair (next head): `reference_documents` + joins + `document_sections` with the FTS index — additive only, no backfill, no changes to existing tables. Deploy is ordinary migrate-up; rollback is migrate-down (drops only new tables; documents stored in the blob store remain orphaned but harmless, matching the attachment blob precedent). The `references/` mount and new tool are inert until a workspace uploads its first document. Smoke gains sections exercising upload → index → search → scoped read → manifest → visibility filtering; integration tests need `DATABASE_URL` as usual.

## Open Questions

- None blocking. Deferred to implementation within the locked design: exact manifest budget figure (tune against the prompt budget in compose), mount collision suffix format, and the FTS `websearch_to_tsquery` vs plaintoxquery choice (decide behind the search tool's single query path with tests).
