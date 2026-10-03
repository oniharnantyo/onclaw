# Spec Delta

## Purpose

Compiles completed agent runs — specifically long, recovery-shaped tool-call sequences — into curated, agent-scoped skills through a gated background loop: qualify, maintain a workspace skill wiki, propose, human approval, materialize, and keep the resulting catalog clean over time.

## ADDED Requirements

### Requirement: Curation consumes the shared turn-ingest stream
The skill-curation pipeline SHALL consume the same turn-end ingest jobs the memory pipeline consumes, as an independently registered consumer. A failure anywhere in the curation pipeline SHALL leave the raw session events untouched, SHALL NOT affect the run that produced the turn, and SHALL NOT affect memory ingestion. When a run qualifies, the pipeline SHALL emit a `skill_candidate` session-event kind so surfaces can badge pending work.

#### Scenario: Curation failure is invisible to runs and memory
- **WHEN** the curation side-call model is unavailable or the pipeline errors while processing a job
- **THEN** the run that produced the turn is unaffected, the memory pipeline still processes the same job and emits its chip, and the raw session events remain intact

#### Scenario: Qualifying run emits a candidate signal
- **WHEN** a completed run passes every qualification gate
- **THEN** a `skill_candidate` session event is emitted for that run's transcript, regardless of whether a proposal is eventually drafted

### Requirement: Qualification gates
A run SHALL trigger wiki-maintenance and proposal work only when all of the following hold, evaluated from existing session-event payloads without any model call: the run completed successfully; it contains at least a configurable minimum of tool calls (default 8); it used at least two distinct tools; it contains at least one recovery event (a tool result flagged as error followed by a later non-error result for the same tool or task step); and it ended with a final assistant message. Runs failing any gate SHALL NOT trigger model calls, and the thresholds SHALL be workspace-configurable.

#### Scenario: Trivial run produces nothing
- **WHEN** a completed run made two tool calls with no errors
- **THEN** no side-call is spent and no candidate work is scheduled for it

#### Scenario: Recovery shape is required
- **WHEN** a completed run made fifteen tool calls across five tools but every call succeeded on the first attempt
- **THEN** the run does not qualify

#### Scenario: Thresholds are workspace config
- **WHEN** an operator raises the minimum tool-call threshold from 8 to 20
- **THEN** a nine-call run that previously qualified no longer triggers proposal work

### Requirement: Failed runs join clusters without triggering
Qualification SHALL gate proposal triggers, not cluster membership: every run SHALL be indexed into its similarity cluster by task shape regardless of gate outcome. A failed run that shares a task family with successful runs SHALL be usable as contrast evidence when a proposal is later drafted from that cluster.

#### Scenario: Failed sibling feeds a later proposal
- **WHEN** a failed run and a later successful run share a task family and the successful one qualifies
- **THEN** the drafted proposal may cite the failed run's sequence as the contrast the procedure encodes

### Requirement: Cluster gate before drafting
A proposal SHALL be drafted only when its similarity cluster contains at least a configurable minimum of qualifying runs (default 2) whose traces converge on the same procedure. A first-ever qualifying run SHALL open or extend a cluster without proposing. Near-duplicate detection SHALL run before drafting: material substantially duplicating an existing candidate or skill SHALL extend that cluster instead of producing a new proposal.

#### Scenario: First qualified run opens a cluster
- **WHEN** the first qualifying run for a task family completes
- **THEN** no proposal is drafted, and the run's window is held as cluster evidence

#### Scenario: Convergence triggers one proposal
- **WHEN** a second qualifying run in the same cluster completes and the traces converge
- **THEN** exactly one proposal is drafted for the cluster

### Requirement: Skill wiki maintenance
Each cycle SHALL maintain a per-workspace skill wiki before any proposal is drafted: sampled failing and passing windows from qualified clusters are analyzed and distilled into pattern pages (failure modes, strategies, workarounds), each page citing the evidence runs it was derived from. Maintenance SHALL supersede or merge stale pages rather than delete them — superseded pages SHALL remain readable with a pointer to their successor. The wiki SHALL be append-only in its logs and SHALL never be rolled back by a rejection. The wiki SHALL NOT be exposed to agent execution: no agent run's composed context SHALL include wiki content.

#### Scenario: Stale pattern is superseded, not erased
- **WHEN** newer evidence contradicts an existing pattern page
- **THEN** the page is superseded by a newer one, the old page remains readable with a successor pointer, and nothing is removed from the wiki

#### Scenario: Rejection leaves the wiki intact
- **WHEN** a human rejects a proposal
- **THEN** the wiki and its pattern pages are unchanged, and the rejection is recorded only in the candidate audit trail

#### Scenario: Wiki never reaches an agent
- **WHEN** any agent run composes its execution context
- **THEN** no wiki page, log, or pattern content is included in that context

### Requirement: Proposal drafting reads the wiki and the audit
The proposal step SHALL draft at most one atomic operation per cluster per cycle — a new skill or a superseding edit to an existing curated skill — and SHALL receive: the wiki index and matched pattern pages, the cluster's sampled windows, and the cluster's rejection audit. Clusters whose rejection audit is unresolved SHALL NOT be re-proposed unless materially new evidence has accumulated since the rejection. A draft SHALL be validated before storage: valid slug name, no name collision with any skill at any tier (system names reserved), a description present, declared tool dependencies that exist in the tool catalog, and size bounds. Invalid drafts SHALL be retried within the cycle and then dropped with the failure logged.

#### Scenario: Suppressed cluster stays quiet
- **WHEN** a cluster's proposal was rejected last week and no new qualifying run has joined it
- **THEN** the cycle drafts nothing for that cluster

#### Scenario: Contradicting evidence proposes an edit
- **WHEN** new evidence contradicts an existing curated skill's procedure
- **THEN** the proposal is an edit to that skill (recorded as superseding its content), not a second skill

#### Scenario: Draft naming a nonexistent tool is rejected
- **WHEN** a draft declares a tool dependency absent from the tool catalog
- **THEN** the draft is not stored as a candidate and the cycle logs the validation failure

### Requirement: Human approval gate
No skill SHALL be created without explicit approval by a workspace Owner or Admin. Approval SHALL materialize the skill as an agent-tier skill — a directory containing SKILL.md and a provenance file mapping it to its cited wiki patterns — written atomically into the owning agent's skills directory, with no registry row, becoming attachable on that agent's next execution. Rejection SHALL record the reviewer-selected reason and persist the audit entry; rejection SHALL suppress the cluster per the proposal rule. Approval SHALL remain possible even when the source session has been deleted.

#### Scenario: Approval creates a mounted skill
- **WHEN** a workspace admin approves a pending proposal
- **THEN** the skill directory is written atomically under the owning agent's skills directory and the agent's next execution lists the skill

#### Scenario: Rejection requires a reason
- **WHEN** a reviewer rejects a candidate without selecting a reason
- **THEN** the rejection is not recorded until a reason is chosen, and the reason lands in the audit entry

#### Scenario: Deleted source does not block approval
- **WHEN** the evidence run of a pending candidate has been deleted
- **THEN** the candidate still lists in review with a "source no longer available" note and can be approved

### Requirement: Curated skill probation and catalog hygiene
A newly approved curated skill SHALL be provisional for a configurable probation window, during which its usage outcomes are tallied: runs that attached it are classified helpful or harmful. When the harmful ratio breaches a workspace-configured threshold, the skill SHALL be automatically disabled and returned to the review surface with the tally. Disabled curated skills SHALL be archived with their evidence linkage intact — removable from every agent's catalog without deletion of history. Approval SHALL enforce a per-agent catalog budget for curated skills; at capacity, approval SHALL be refused with guidance to disable or merge. Skill content SHALL be revised by superseding edits only — never by accumulating sibling skills for the same procedure.

#### Scenario: Probation breach auto-disables
- **WHEN** a provisional skill accumulates more harmful than helpful outcomes past the minimum sample
- **THEN** it is disabled automatically, disappears from the agent's catalog, and reappears in the review surface with the tally

#### Scenario: Budget refusal names the remedy
- **WHEN** an admin approves a candidate while the agent's curated catalog is at budget
- **THEN** approval is refused with guidance naming existing skills to disable or merge first

#### Scenario: Disabled skill keeps its history
- **WHEN** a curated skill is disabled and later inspected
- **THEN** its evidence runs, cited patterns, and outcome tally remain queryable

### Requirement: Cycle cadence and manual trigger
The curation cycle SHALL run on a workspace-configurable schedule (default nightly) and SHALL be triggerable on demand by a workspace Owner or Admin. Wiki maintenance SHALL run every cycle regardless of pending human verdicts. A failed cycle SHALL log its failure and retry at the next tick without affecting anything user-facing. All qualifier thresholds, score weights, budget, and the catalog budget SHALL be workspace-level configuration.

#### Scenario: Manual trigger runs a cycle now
- **WHEN** an admin presses the run-curation-now control
- **THEN** one cycle executes immediately, and its status (running, succeeded, failed) is observable

#### Scenario: Pending verdicts do not stall maintenance
- **WHEN** candidates await human review at cycle time
- **THEN** wiki maintenance still runs; only proposal materialization waits on verdicts

### Requirement: Curation model resolution
The two model side-calls (wiki maintenance, proposal) SHALL resolve their model per agent: the agent's explicit curation pair if set, otherwise the agent's memory side-call pair, otherwise the workspace default, otherwise the provider default. Leaving the curation pair unset SHALL produce no behavior change beyond falling through that order.

#### Scenario: Curation pin overrides memory's model
- **WHEN** an agent sets a curation model pair distinct from its memory side-call pair
- **THEN** both curation side-calls run on the curation pair while memory side-calls continue on the memory pair

#### Scenario: Unset falls through
- **WHEN** no curation pair is configured anywhere
- **THEN** curation side-calls run on the same model memory side-calls resolve to
