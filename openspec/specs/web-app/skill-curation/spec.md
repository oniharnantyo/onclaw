# web-app/skill-curation Specification

## Purpose

The review and monitoring surfaces for the skill-curation loop: a Curation tab in workspace settings carrying candidates, patterns, and audit; the candidate review takeover; provenance and probation chips on agent cards; and the in-chat candidate signal.

## Requirements

### Requirement: Curation tab in skills settings
The Skills settings page SHALL present two top-level tabs — **Skills** (the existing skill library, unchanged as the default view) and **Curation**. The Curation tab SHALL carry a pending badge reflecting the count of candidates awaiting review, SHALL expose the cycle status (schedule, last run time and outcome, pattern and proposal counts), and SHALL offer a run-curation-now control available to workspace Owners and Admins, with a visible running state while a cycle executes. Inside the tab, three sub-tabs SHALL be provided: **Candidates**, **Patterns**, and **Audit**.

#### Scenario: Badge reflects pending work
- **WHEN** two candidates await review
- **THEN** the Curation tab shows a badge of 2, and it clears when the queue empties

#### Scenario: Empty states name the mechanism
- **WHEN** the Candidates sub-tab has no entries
- **THEN** the empty state explains that candidates appear when agents solve similar problems repeatedly

### Requirement: Candidates list
The Candidates sub-tab SHALL list pending and provisional work as cards showing: the proposed skill name, whether the proposal is a create or an edit (and of which skill), status (pending, provisional with probation day count), a muted provenance line (owning agent, cluster run count, tool-call count, recovery count), and cited-pattern references. Provisional cards SHALL show their outcome tally (helpful, harmful, mounted-run count). Cards whose extraction failed SHALL show an error chip with a retry action.

#### Scenario: Edit proposal is distinguishable
- **WHEN** the proposal revises an existing curated skill
- **THEN** the card reads as an edit naming that skill, not as a new skill

#### Scenario: Provisional card shows its tally
- **WHEN** a curated skill is mid-probation
- **THEN** its card shows day count within the window plus helpful and harmful counts

### Requirement: Candidate review takeover
Opening a candidate SHALL present a takeover with, on one side, the proposed content — a readable preview for creates, a before/after diff against the current skill content for edits — and on the other side, the trust surface: the cluster's evidence runs (deep-linked to their transcripts, with tool-call and recovery summaries; a deleted source shows "source no longer available" without blocking action) and a cited-patterns panel where each pattern opens the wiki page and from there its own evidence runs. The footer SHALL offer Approve and Reject; Reject SHALL require choosing a reason (wrong procedure, already covered, contradicts newer information, other) before recording. Approval SHALL be limited to workspace Owners and Admins; other viewers see the same content without action affordances.

#### Scenario: Approver walks the evidence chain
- **WHEN** a reviewer opens a cited pattern from the takeover
- **THEN** the wiki page opens showing its evidence runs, without leaving the review context

#### Scenario: Reject without reason is blocked
- **WHEN** a reviewer presses Reject with no reason selected
- **THEN** the rejection is not recorded until a reason is chosen

#### Scenario: Edit renders as a diff
- **WHEN** the proposal is an edit to an existing skill
- **THEN** the content pane shows the changed lines against the current version, not a full replacement preview

### Requirement: Patterns tab
The Patterns sub-tab SHALL list the workspace's wiki pages read-only: title, status (active or superseded with a pointer to the successor), the skills citing each page, and evidence-run links. Superseded pages SHALL remain visible. There SHALL be no pattern-editing affordance; maintenance is the cycle's job.

#### Scenario: Superseded page stays visible
- **WHEN** a pattern has been superseded by a newer page
- **THEN** it still lists, marked superseded, with a link to its successor

### Requirement: Audit tab
The Audit sub-tab SHALL list proposal outcomes for a configurable retention window (default 30 days): each entry showing the proposal summary, its diff, the verdict (approved or rejected), the reviewer-selected reason when rejected, and the cluster it belonged to. Entries SHALL be read-only.

#### Scenario: Rejected proposal remains inspectable
- **WHEN** a reviewer wants to know why a procedure was rejected last week
- **THEN** the audit entry shows the diff, the verdict, and the recorded reason

### Requirement: Agent-card provenance and probation chips
On an agent's Skills pane, a curated skill SHALL display: a curated badge with its version, its cited-pattern references, and — while provisional — its probation state with the outcome tally. A promote-to-workspace affordance SHALL be shown disabled with its unmet criteria named (for example, requiring convergence across multiple agents); enabling it is out of scope for this change.

#### Scenario: Provisional skill shows its standing
- **WHEN** a member views an agent's skills while one is provisional
- **THEN** the row shows the probation day count and its helpful/harmful tally

### Requirement: In-chat candidate chip
When a run qualifies, its transcript SHALL render a candidate chip — styled consistently with the existing memory-ingested chip — stating that a skill candidate was drafted from this run, linking to the candidate review surface for those with approval rights.

#### Scenario: Chip appears on the qualifying run
- **WHEN** a user reopens the transcript of a run that passed all qualification gates
- **THEN** the transcript shows the candidate chip linking to review
