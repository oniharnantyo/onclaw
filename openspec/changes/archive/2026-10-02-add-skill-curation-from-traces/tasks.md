# Tasks

## 1. Ingest seam extraction (task 0 — mechanical, lands green alone)

- [x] 1.1 Create `internal/ingest` with `Job` (moved from `memory.IngestJob`), `Worker` (bounded queue + per-session serialization moved verbatim from `internal/memory/worker.go`), and `Consumer { Ingest(ctx, ingest.Job) error }`; move the worker tests with it. Verify: `go build ./...` and `go test ./internal/ingest/...` green.
- [x] 1.2 Register memory as consumer #1 in the composition root: its pipeline (gister → gate → notes → embed) wrapped as an `ingest.Consumer` that owns the `x.memory_ingested` ChipSink unchanged; update `Runner.enqueueTurnIngest` to target `ingest.Worker`. Verify: `go test ./internal/memory/... ./internal/agents/...` green; memory chip event kind byte-identical in a fixture test.
- [x] 1.3 Add consumer fault isolation: one consumer's panic or error is logged and cannot fail the dispatch of the other. Verify: fake-consumer unit test where consumer #1 panics and consumer #2 still receives its job.

## 2. Domain, config, and store

- [x] 2.1 Add `SkillCurationProviderID`/`SkillCurationModel` optional pair to `domain.Agent` with the same validation rules as the memory side-call pair, expose in the agent config API PATCH, and add the resolution order (curation pair → memory side-call pair → workspace default → provider default) next to the existing side-call resolver. Verify: unit test for the four-step fallback and for validation rejection of an unknown provider/model.
- [x] 2.2 Add workspace-level curation config (qualifier thresholds: min tool calls, min distinct tools, cluster minimum, probation window + harmful-ratio threshold, catalog budget, nightly budget K, cycle interval) with conservative defaults per the spec. Verify: config assembly test; unset config yields spec defaults.
- [x] 2.3 Create the migration for `skill_candidates` and `skill_impact_entries` (status, proposed content, diff linkage, evidence event IDs, cited pattern refs, cluster ID, verdict + reason, timestamps) plus the `SkillCandidateStore` sub-interface, postgres adapter, and in-memory fake. Verify: integration test against DATABASE_URL for candidate lifecycle inserts; fake passes the same contract tests.

## 3. Qualifier and clustering

- [x] 3.1 Implement the qualifier in `internal/skillcuration` as an `ingest.Consumer`: scan the job's session window for the five hard gates using existing `IsError`/`Latency`/tool-name fields; emit `x.skill_candidate` session event on qualification; index every run (pass or fail) into its similarity cluster. Verify: table-driven unit tests over synthetic event windows covering each gate's pass/fail boundary, the recovery-sequence matcher, and the two-tier rule (failed run indexed, not triggered).
- [x] 3.2 Implement the nightly top-K budget: rank qualified jobs by the soft score (recovery count decay, latency effort, tool diversity, cluster-growth priority, origin weight). Verify: unit test on score ordering; budget caps side-call count per cycle.

## 4. Skill wiki

- [x] 4.1 Implement the wiki file layer: `<ONCLAW_DIR>/workspaces/<slug>/skillwiki/{patterns/,logs.md}` with atomic page writes, supersede-with-successor-pointer, and append-only logs. Verify: unit tests for create/supersede/merge flows; a superseded page remains readable with a pointer.
- [x] 4.2 Implement the maintainer side-call: sample stratified failing/passing windows from qualified clusters, prompt-bounded per `budget.go` precedent, resolve the model via the D7 order, parse output into page operations executed by Go code. Fail-soft: log and defer on error. Verify: fake-model unit test for parse-and-apply; failure path leaves wiki consistent.

## 5. Proposal and candidates

- [x] 5.1 Implement the proposer side-call: input = wiki index + matched patterns + sampled cluster windows + rendered rejection audit from the store; enforce one atomic create-or-edit per cluster per cycle; suppress clusters with unresolved rejections absent materially new evidence. Verify: fake-model unit tests — suppression honored, edit proposed over sibling when evidence contradicts an existing curated skill, second-skill proposals for the same procedure rejected in code.
- [x] 5.2 Implement draft validation (slug rules, cross-tier collision with system names reserved, description present, `dependencies.tools` ⊆ tool catalog, size bounds) with bounded retry then drop-and-log. Verify: table-driven validation tests including the system-name reservation and unknown-tool rejection.
- [x] 5.3 Emit the candidate row with evidence IDs, cited patterns, and cluster linkage; emit the `x.skill_candidate` chip (already covered in 3.1) and surface candidate count for the badge API. Verify: store round-trip test; badge-count endpoint test.

## 6. Approval and materialization

- [x] 6.1 Implement approve/reject endpoints guarded by the existing Owner/Admin permission middleware: approve = re-validate collisions → mkdir under `AgentSkillsDir` → atomic write of `SKILL.md` + `PURPOSE.md` (pattern links) using the promptdocs write discipline; reject = require reason, store impact entry, suppress cluster. Verify: endpoint tests for both paths incl. race-collision rejection, missing-reason rejection blocked, and deleted-source-session approval allowed.
- [x] 6.2 Implement provisional lifecycle: usage counters join skill attach/read telemetry with terminal run outcomes; harmful-ratio breach auto-disables and returns the candidate to review with the tally; catalog budget enforced at approval with named remedies; disabled = archived, evidence retained. Verify: fake-store lifecycle tests — probation breach transition, budget refusal message, archive retains evidence linkage.

## 7. Cycle orchestration

- [x] 7.1 Clone the scheduler claim-loop into the curation cycle service: workspace-configurable interval (default nightly), manual trigger entry point, cycle status recording. Verify: unit test for claim/trigger; two triggers don't double-run concurrently.
- [x] 7.2 Wire the full cycle: qualify backstop → wiki maintenance → proposals → status write; fail-soft at every stage with morning-report counters (extraction failures, qualification rate, approval rate, pattern count, catalog growth). Verify: integration test running one full cycle over seeded events produces wiki pages + candidate + status; a failing side-call stage defers without blocking the next tick.

## 8. Withholding invariant and APIs

- [x] 8.1 Add the compose-time withholding guard and test: wiki paths are excluded from every `ComposeParams` document source; a regression test asserts wiki content never appears in composed instructions. Verify: compose test over an agent with a populated wiki.
- [x] 8.2 Expose the read APIs for the UI: candidates list/detail (with rendered evidence + cited patterns), patterns list, audit list, cycle status/trigger. Verify: handler tests with permission matrix (Member read-only, Owner/Admin actions).

## 9. Web surfaces

- [x] 9.1 Rebuild the Skills settings page as the two-tab shell ([Skills] unchanged default view, [Curation] with pending badge) per the settings delta. Verify: component test for tab shell, badge count from the API, identical library behavior with empty curation.
- [x] 9.2 Build the Candidates list and review takeover: create/diff toggle, evidence runs with recovery summaries and deleted-source note, cited-patterns panel linking wiki pages, approve/reject with reason picker. Verify: component tests for diff rendering, reason-required rejection, permission-gated actions.
- [x] 9.3 Build Patterns and Audit sub-tabs (read-only; superseded pointers; audit with verdicts and reasons). Verify: component tests for superseded rendering and audit row content.
- [x] 9.4 Add agent-card provenance/probation chips and the disabled promote affordance with named criteria; add the in-chat `x.skill_candidate` chip on qualifying transcripts. Verify: component tests for chip states (provisional tally, graduated, disabled); chat test for chip rendering from the session event.

## 10. End-to-end verification

- [x] 10.1 Extend `scripts/smoke.sh` with a curation section: seed a qualifying multi-tool trace → run a cycle manually → candidate appears → approve via API → skill directory exists with PURPOSE.md → next agent run lists the skill → wiki pages + audit entry present. Verify: smoke section passes green (full suite, fresh onclaw_smoke DB).
- [x] 10.2 Run `openspec validate --strict` on the change and reconcile any deltas against the specs. Verify: strict validation passes.
- [x] 10.3 Full verification pass: `go build ./... && go vet ./... && go test ./...`, integration tests with DATABASE_URL, web test suite, and smoke suite. Verify: all green; memory pipeline behavior unchanged (existing memory specs' tests untouched and passing).
