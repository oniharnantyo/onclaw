# Proposal

## Why

Agents routinely solve problems through long tool-call sequences — failures, retries with changed arguments, eventual success — and the procedure they discovered evaporates when the run ends. The literature (WikiSkill, SkillOS, CoEvoSkills, MetaSkill-Evolve) shows that compiling such traces into curated skills is the highest-value self-improvement lane, and that the decisive factor is *curation quality*: unverified extraction produces garbage skills that tax every run. OnClaw already stores every trace durably (`session_events`), already has a background ingestion seam (the memory pipeline's `IngestJob`), and already mounts agent-scoped skills by directory scan — so the loop can be built almost entirely on existing infrastructure, with quality gates carried by the human approval flow that already governs this workspace.

## What Changes

- **New `internal/ingest` seam (mechanical, no behavior change):** the turn-end job queue is extracted out of the memory worker into a neutral `ingest.Job` / `ingest.Worker` / `ingest.Consumer` package; memory becomes registered consumer #1 (its pipeline and `x.memory_ingested` chip unchanged), the new skill-curation consumer registers as #2 with its own `x.skill_candidate` chip. Per-consumer fault isolation is new.
- **New skill-curation pipeline** as a background consumer with two tiers: a zero-LLM *qualifier* (shape heuristics over event payloads: completion status, tool-call counts, distinct tools, `IsError` recovery sequences, final turn) that gates everything, followed — only for qualified runs — by two bounded side-LLM calls: *wiki maintain* (root-cause sampled failing/passing windows into pattern pages) and *propose* (draft one skill create-or-edit, informed by the wiki index, matched patterns, and the rejection audit).
- **New skill wiki:** a per-workspace `skillwiki/` directory of pattern pages plus an append-only `logs.md`, maintained by the cycle, never agent-facing, never rolled back — superseded pages stay visible with pointers.
- **New candidate store with audit trail:** proposed drafts land in a `SkillCandidateStore` with status (`pending/approved/rejected`), evidence event IDs (provenance-at-birth, same discipline as memory notes), cited pattern links, and impact entries recording every proposal's diff and verdict — rejected proposals suppress their cluster until materially new evidence arrives.
- **New human approval gate:** every candidate requires explicit approval (workspace Owner/Admin); approval writes `SKILL.md` + `PURPOSE.md` atomically into `AgentSkillsDir` — no registry row, mounted by the existing SkillBackend on the next run. Rejection persists the reason; the wiki is never gated and never rolled back.
- **New anti-garbage mechanics:** curated skills start *provisional* (probation window with helpful/harmful outcome counters joined from run results; auto-disable on a harmful-ratio breach), a hard per-agent catalog budget, edit-over-sibling (contradicting evidence proposes a superseding edit, never a second skill), and archived-not-deleted retention.
- **New UI surfaces:** the workspace Skills settings page becomes two tabs — the existing gallery unchanged under **Skills**, and a new **Curation** tab (Candidates / Patterns / Audit sub-tabs, pending badge, cycle status, manual "Run curation now") with a candidate review takeover (draft/diff view, evidence runs, cited patterns panel, reject reason picker), provenance and probation chips on agent cards, and an in-chat candidate chip on qualifying runs.
- **New config surface:** optional `SkillCurationProviderID`/`SkillCurationModel` pair on the agent (same pattern as the memory side-call pair; falls through to it when unset), plus workspace-level qualifier thresholds, catalog budget, and cycle interval (default nightly).

Deliberately out of scope: playbooks, IDENTITY/SOUL writes, workspace-tier skill production (promotion is a future, evidence-gated change), shadow-eval auto-approval, an agentic maintainer with trace tools, outcome-trained curation policies.

## Capabilities

### New Capabilities

- `skill-curation`: the background self-improvement loop — qualification gates, wiki maintenance, proposal drafting, candidate store with impact audit, human approval, agent-tier skill materialization, probation/catalog-budget hygiene, cadence and manual trigger, agent-facing withholding invariant, and the SkillCuration model knob.
- `web-app/skill-curation`: the Curation tab (Candidates / Patterns / Audit), review takeover with diff and cited-patterns panel, reject reason picker, cycle status and manual trigger control, agent-card provenance/probation chips, and the in-chat candidate chip.

### Modified Capabilities

- `web-app/settings`: the Skills settings pane becomes a two-tab shell — the existing skill-library pane moves under a **Skills** tab unchanged, and a new **Curation** tab is added carrying the pending-work badge and the curation surfaces above.

## Impact

- **Code (new):** `internal/ingest`, `internal/skillcuration`, a `SkillCandidateStore` store sub-interface with postgres adapter + in-memory fake + migration, curation service wiring in the composition root, HTTP endpoints (list/get/approve/reject candidates, cycle status/trigger), web settings tab + takeover components.
- **Code (touched, no behavior change):** `internal/memory/worker.go` (queue extraction), `internal/agents/runner.go` (`enqueueTurnIngest` target), `internal/domain/agent.go` (two optional config fields + validation), agent config API PATCH.
- **Wire contracts:** one new session-event kind (`x.skill_candidate`); `x.memory_ingested` unchanged.
- **Storage:** one new migration (candidate + impact tables); one new on-disk tree (`<ONCLAW_DIR>/workspaces/<slug>/skillwiki/`).
- **Cost:** exactly two side-LLM calls per qualified run on the SkillCuration model; qualification, clustering signals, approval, writes, and mounting are all LLM-free. No change to run-path latency or outcomes (fail-soft, off the hot path).
- **Specs:** `agent-memory-pipeline` and `workspace-skills` requirements are untouched — the memory enqueue semantics are identical and agent-tier skills remain unregistered directories per the existing tier rules; the new capability owns all new behavior.
