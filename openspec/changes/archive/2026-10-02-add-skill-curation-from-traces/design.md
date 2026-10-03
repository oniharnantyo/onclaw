# Design

## Context

The memory pipeline already owns everything the curation loop needs to ride on: a turn-end `IngestJob` queue with per-session serialization (`internal/memory/worker.go`, fed by `Runner.enqueueTurnIngest`), incremental trace windows re-parsed from `session_events` (`gister.go` + `HumanReadableSerializer`), a cheap-model side-call seam with a proven resolution order (`gate.go`, `agent.MemorySidecallProviderID/Model`), embedding similarity (`CountSimilar`), and a nightly consolidator. Skills already mount by directory scan (`internal/agents/backend/skill_backend.go` — a skill is a directory containing SKILL.md; agent tier is unregistered by spec), and `TranscriptEvent` already carries `IsError` and `Latency` on `tool_call_finished` (events.go:116), so every qualifier input exists today. The design therefore adds one new package, one store, and file formats — not a new execution substrate. See proposal.md for motivation and the spec deltas for behavior.

## Goals / Non-Goals

Goals:
- The loop runs entirely off existing turn-end ingestion — no new triggers, no hot-path work, no new execution machinery.
- Models draft text; Go code and humans materialize everything (no agent ever holds a file-write tool for skills).
- Every stored artifact (candidate, pattern, skill) carries evidence pointers back to `session_events` from birth.
- The memory pipeline's behavior, wire contracts, and specs are untouched.

Non-Goals:
- Workspace-tier or system-tier skill production; promotion UI (affordance ships disabled).
- Shadow evaluation / auto-approval; PACE-style statistical acceptance.
- Agentic maintainer with on-demand trace tools (v1 maintainer is a bounded side-call).
- Editing curated skills outside the loop's supersede grammar (no free-form wiki editor).

## Decisions

**D1 — Extract the ingest seam first (task 0), as `internal/ingest`.**
`Job` (was `IngestJob`), `Worker` (queue + per-session serialization only), and `Consumer { Ingest(ctx, Job) error }`, registered in the composition root. Memory becomes consumer #1 owning its `x.memory_ingested` ChipSink; skillcuration is consumer #2 owning `x.skill_candidate`. Alternative considered: keep the worker in `internal/memory` with a consumer interface — rejected because every future post-run consumer would import `memory` for non-memory work, and because per-consumer fault isolation falls out of the boundary for free. `Runner.enqueueTurnIngest` keeps its name (already neutral); the `x.memory_ingested` event kind is a wire/persistence contract and does not change. This is a purely mechanical refactor committed green before any curation code exists.

**D2 — Qualifier is pure heuristics over existing payload fields.**
Five hard gates (completed status; ≥8 tool calls; ≥2 distinct tools; ≥1 `IsError`→non-`IsError` recovery on the same tool/step; final assistant turn) computed by scanning the job's session window — the same tally pattern `scheduler.drainRun` uses. Soft score ranks the nightly budget. Two-tier rule: every run is cluster-indexed; only gate-passing runs trigger side-calls. Rationale: the qualifier is the cost gate — keeping it LLM-free caps worst-case spend at two calls per qualified run and makes qualification rate a pure config question. Alternatives (LLM-based triage, embedding-first gating) add cost before value; rejected.

**D3 — The wiki is files on disk, not memory rows.**
`<ONCLAW_DIR>/workspaces/<slug>/skillwiki/` with `patterns/*.md` and append-only `logs.md`. MemoryNote could structurally host patterns (visibility, topics, supersede chains, tombstones), but memory is agent-facing — the prefetch/compose paths would become leak surfaces for content that must never reach a run (WikiSkill's withholding ablation shows leaking costs quality). Files match the skills-on-disk precedent, make the wiki user-inspectable, and make "never rolled back" a filesystem property. Supersede = write a successor page + pointer in the old one; merging and staleness are the cycle's job. The audit trail (`skill-impact.md` in the paper) is deliberately NOT a file here: impact entries live in the candidate store (DB) — the system of record — and are rendered into the proposer's prompt; the DB gives the Audit tab for free and avoids dual-write drift.

**D4 — Proposal grammar: one atomic create-or-edit per cluster per cycle.**
The proposer is a bounded side-call receiving wiki index + matched patterns + sampled windows + the rendered rejection audit. Its output is validated by Go (slug rules, cross-tier collision with system names reserved, description present, `dependencies.tools` ⊆ tool catalog, size bounds) before becoming a candidate row. The edit-over-sibling rule is enforced in the prompt contract and re-checked in code: contradicting evidence produces a superseding edit against the existing skill (content versioned, prior content preserved in the candidate history), never a second skill. House SKILL.md format verified from the embedded system skills: H1 + `name:`/`description:` header lines, optional `---` frontmatter with `description` and `dependencies.{tools,binaries,python}` (already parsed by skill_backend). Curated skills additionally carry `PURPOSE.md` mapping to cited patterns — on-disk provenance that survives store retention windows.

**D5 — Approval is the oracle; creation is one atomic write.**
Approve (Owner/Admin, reusing existing permission guards) re-validates collisions, then `mkdir` + atomic temp-rename-backup write of `SKILL.md` + `PURPOSE.md` under `AgentSkillsDir` — the `promptdocs` write discipline. No registry row (agent tier is unregistered by `workspace-skills` spec — creation stays conformant). Deleted source sessions show a note but do not block. Rejection stores reviewer reason + diff + cluster linkage; the suppressed cluster re-opens only on materially new evidence (new qualifying run whose window diverges from the rejected draft).

**D6 — Probation, budget, and retention ride the candidate store from day one.**
Approved skills start `provisional`; usage counters join SkillBackend read/attach telemetry with terminal run outcomes (the runner already knows both). Harmful-ratio breach → auto-disable → candidate row back to review with the tally. Catalog budget enforced at approval time with named remedies. Disabled = archived (retained evidence linkage, zero catalog tokens), never deleted. Health metrics (qualification rate, approval rate, pattern count, catalog growth) land in the consolidator morning report alongside the existing extraction-failure counts.

**D7 — Cadence is a claim-loop, cloned from the scheduler.**
A per-workspace ticker (nightly default, workspace-configurable) claims due cycles and runs: qualify backstop → wiki maintenance → proposal for unsuppressed clusters → human gate waits externally. The same entry point serves the manual trigger and is what the cycle-status API reads. Fail-soft: any stage failure logs and defers to the next tick.

## Risks / Trade-offs

- [Wrong pattern pages poison every later proposal — WikiSkill doesn't gate the wiki, and our gate is human attention] → patterns always cite evidence runs; the review takeover renders cited patterns so approvers judge the chain; maintenance supersedes rather than accumulates; pattern count is a health metric.
- [Human approval fatigue kills the feature] → cluster suppression on rejection, tight qualifier gates, nightly top-K budget, and a deliberately small catalog keep the queue short; approval rate is alarmed (<20% ⇒ thresholds retuned).
- [Side-call cost on noisy workspaces] → qualification is free and strict; the nightly budget caps side-calls; qualification-rate alarm (>15% of runs qualifying) flags loose thresholds.
- [Wiki grows unbounded (the paper's own gap)] → consolidator-cadence merge/supersede; superseded pages retained but marked; no hard cap in v1, monitored via morning report.
- [Ingest extraction regresses memory behavior] → task 0 is mechanical and lands green before curation exists; memory pipeline specs/scenarios unchanged; `x.memory_ingested` contract untouched.
- [Curated skill conflicts with newer memory facts] → consolidator cross-checks skill dependencies/patterns against newer notes and flags stale skills for superseding edits (flag-only in v1).

## Migration Plan

1. Land task 0 (`internal/ingest` extraction) as an isolated, green refactor — deployable alone, rollback = revert.
2. Add store migration (candidates + impact entries) and the skillcuration consumer behind the existing composition root wiring; default schedule nightly; no UI until step 3 — the loop runs headless and harmlessly (candidates accumulate, nothing user-facing).
3. Ship endpoints + web Curation tab + chips; approve flow goes live.
4. Rollback: disable the curation consumer registration (composition-root flag) — candidates and wiki become inert data; skills already materialized remain ordinary agent-tier skills removable by deletion.

## Open Questions

- Exact chip visual treatment for `x.skill_candidate` in the transcript (reuse memory-chip component vs a distinct icon) — UI-level, decidable at implementation.
- Whether the nightly cycle should also backfill clusters from historical sessions on first enable, or start from enable-day only (spec allows either; start-from-enable is the conservative default and needs no task).
