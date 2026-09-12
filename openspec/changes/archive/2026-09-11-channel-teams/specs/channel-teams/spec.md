# channel-teams delta

## Purpose

Team-layer collaboration on the channel substrate: bounded work sessions with human gates, a facilitator role that owns termination and stalls, a shared project directory for real artifacts, and built-in team templates that materialize role-specialized channels.

## ADDED Requirements

### Requirement: Work sessions
A human member MAY flag a channel message as a **kickoff**; the chokepoint SHALL then mint a work session rooted at that message with a goal (the kickoff text) and a hop budget (default 12). Every agent run minted inside an open session SHALL consume one hop and its originating feed message SHALL link to the session. A session SHALL be exactly one of `open`, `paused` (with reason: `awaiting-human` or `budget-exhausted`), or `closed` (with the facilitator's summary). When the budget exhausts, the session SHALL pause and the facilitator SHALL be summoned to post a status; work resumes only when a human member posts to the channel or the session is closed.

#### Scenario: Kickoff opens a session
- **WHEN** Sarah posts "Add dark mode to settings" flagged as a kickoff in a channel with a facilitator
- **THEN** a work session is minted rooted at that message and the facilitator is summoned to plan

#### Scenario: Hops are budgeted
- **WHEN** agents exchange ten mentions inside an open session and two hops remain
- **THEN** the next summon consumes a hop and, once the budget is empty, the session pauses for the facilitator status

#### Scenario: Budget exhaustion pauses
- **WHEN** the hop budget reaches zero
- **THEN** the session pauses as `budget-exhausted` and no further agent runs are minted for it until a human posts

### Requirement: Session bounds replace chain caps
Inside an open work session, an agent-authored mention of an agent member SHALL summon deterministically subject only to the session budget — the v1 chain-depth cap and no-re-summon rule SHALL NOT apply to in-session traffic. Outside sessions, the v1 caps SHALL apply unchanged.

#### Scenario: Deep handoff chain inside a session
- **WHEN** an in-session reply chain runs PM → architect → backend → tester → scrum master
- **THEN** every hop summons its agent as long as the budget allows

#### Scenario: Casual traffic stays capped
- **WHEN** an agent reply outside any session mentions another agent already summoned in that chain
- **THEN** the v1 no-re-summon cap suppresses the summon

### Requirement: Human gates
When an agent message inside an open session mentions a human member, the session SHALL pause as `awaiting-human` and the awaiting state SHALL surface to the mentioned human (in-app channel mention state). A subsequent message posted by a human member in the channel SHALL resume the session. The pause/resume transitions SHALL be visible as session status.

#### Scenario: Agent asks for sign-off
- **WHEN** the PM agent posts "approve to start implementation?" mentioning Sarah inside an open session
- **THEN** the session pauses as `awaiting-human` and Sarah's channel shows the awaiting state

#### Scenario: Human reply resumes
- **WHEN** Sarah posts "approved" in the channel while the session is `awaiting-human`
- **THEN** the session resumes and the message joins the session feed

### Requirement: Facilitator role
Each channel MAY designate exactly one member — user or agent — as **facilitator**. The facilitator SHALL be summoned on kickoff (to post the opening plan), on budget exhaustion (to post a status), and by the stall watchdog; its runs' hops SHALL count against the session budget like any agent's. Only the facilitator SHALL be able to close a session, via a `session.close` tool whose summary is stored on the session and posted to the feed; the session MUST NOT accept new summons after closing.

#### Scenario: Facilitator closes with a summary
- **WHEN** the facilitator calls `session.close` with a summary inside an open session
- **THEN** the session becomes `closed`, the summary is stored and visible in the feed, and further in-session summons are suppressed

#### Scenario: Only the facilitator closes
- **WHEN** a non-facilitator agent attempts `session.close`
- **THEN** the tool is unavailable to it (not in its toolset) or refuses

### Requirement: Stall watchdog
An open session with no agent activity for an idle period (default 10 minutes) SHALL trigger exactly one watchdog per idle period: the facilitator is summoned with the session state and expected to route to the next specialist, ask a human, or close.

#### Scenario: Stalled session pokes the facilitator
- **WHEN** an open session sees no agent activity for the idle period
- **THEN** the facilitator is summoned once with the session context; if it also idles, no further watchdog fires until activity resumes

### Requirement: Shared project space
Each channel SHALL have a project directory (`projects/<channel-slug>/` under the workspace data root) mounted read-write at `/project` into the filesystem jail of the channel's **member agents only**. File tools SHALL operate on `/project` for members; agents outside the channel SHALL have no `/project`. All other jail rules (escape rejection, dangerous-command governance) SHALL apply unchanged. No built-in conflict resolution beyond conventions is provided in this change.

#### Scenario: Member agent writes an artifact
- **WHEN** a member agent writes `spec.md` under `/project`
- **THEN** the file persists in the channel's project directory and is readable by every other member agent

#### Scenario: Non-member has no project root
- **WHEN** an agent that is not a channel member runs file tools
- **THEN** no `/project` root exists in its jail

### Requirement: Team templates
The system SHALL ship built-in team templates — each a named set of role slots (specialization notes), a conventions prefill, and a designated facilitator slot — starting with "Software Team" (PM, architect, scrum master, frontend, backend, tester). Materializing a template SHALL create the channel and its memberships, binding each slot either to a newly spawned agent (role-informed identity generation) or to an existing workspace agent chosen by the creator.

#### Scenario: Materialize a Software Team
- **WHEN** a user materializes the Software Team template, spawning all six agents
- **THEN** a channel exists with six agent memberships carrying their role specializations, the scrum master set as facilitator, and prefilled conventions

#### Scenario: Bind an existing agent to a slot
- **WHEN** the creator binds the "architect" slot to an existing workspace agent
- **THEN** no new agent is spawned for that slot and the existing agent joins with the slot's specialization

### Requirement: Session context rides the channel document
For runs in a channel with an active (open or paused) work session, the channel document SHALL include the session's goal, status, and remaining hops so every summoned agent knows the current engagement state.

#### Scenario: Agent sees the engagement state
- **WHEN** an agent is summoned mid-session with five hops remaining
- **THEN** its channel document carries the session goal, `open` status, and remaining hops
