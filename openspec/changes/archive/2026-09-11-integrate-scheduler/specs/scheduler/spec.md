## Purpose

The scheduler: workspace-scoped standing orders that run an agent on a recurrence or at a one-shot time, without a human at the keyboard — a named job binding an agent, a task prompt, and a delivery target, fired by a server-side ticker into per-run sessions.

## ADDED Requirements

### Requirement: Scheduler entity
A scheduler SHALL belong to exactly one workspace and bind: a unique-per-workspace name, exactly one agent, a non-empty task prompt, a schedule (recurrence expression or one-shot time), an enabled flag, and a delivery target. Two schedulers in the same workspace and agent SHALL NOT share a name. Scheduler prompts are unstructured text and SHALL be entered as free text, never as structured JSON. The workspace timezone SHALL apply to all schedule interpretation; schedulers carry no timezone of their own.

#### Scenario: Duplicate name rejected
- **WHEN** a user saves a scheduler named `morning-digest` for agent Atlas when another Atlas scheduler in the workspace already has that name
- **THEN** the save is rejected with a field-level uniqueness error and nothing is created

#### Scenario: Workspace isolation
- **WHEN** a member of workspace A lists schedulers
- **THEN** only workspace A's schedulers are returned, and addressing a workspace B scheduler by id is indistinguishable from addressing an unknown scheduler

### Requirement: Recurrence and one-shot schedules
A recurring scheduler SHALL store a standard 5-field cron expression evaluated in the workspace timezone; the system SHALL compute and expose the next fire time and a human-readable label derived from the expression (never user-entered). A one-shot scheduler SHALL store a single target instant and SHALL archive itself after firing (or after being missed beyond a grace window). The next fire time SHALL be visible on every read of a scheduler (absent when paused or archived).

#### Scenario: Weekday mornings
- **WHEN** a scheduler is saved with the expression `0 9 * * 1-5` in a workspace whose timezone is `Asia/Jakarta`
- **THEN** the computed next fire time is the next 09:00 Asia/Jakarta that is a Monday–Friday, and a human-readable label such as "09:00 · Mon–Fri" is derived

#### Scenario: One-shot archives after firing
- **WHEN** a one-shot scheduler's target instant arrives and the run completes
- **THEN** the scheduler no longer appears as schedulable (its next fire time is absent) while its run record remains viewable

#### Scenario: Invalid expression rejected at save
- **WHEN** a user saves a recurring scheduler with the expression `at nine`
- **THEN** the save is rejected with an expression-level error

### Requirement: Scheduler API and permissions
The API SHALL provide workspace-scoped endpoints to list, create, read, update, delete, and run-now schedulers, and to list a scheduler's runs. Reads SHALL require `scheduler.read`; mutations and run-now SHALL require `scheduler.write`. Built-in roles SHALL be granted `scheduler.read` to every member role and `scheduler.write` to Owner and Admin, including a Superadmin's workspace permission set, via an idempotent backfill migration.

#### Scenario: Member cannot create
- **WHEN** a Member-role user POSTs a new scheduler
- **THEN** the response is 403 insufficient permissions

#### Scenario: Read requires the read permission
- **WHEN** a user without `scheduler.read` lists schedulers
- **THEN** the response is 403

### Requirement: Due schedulers fire claimed exactly once
The system SHALL run a server-side scheduler loop that fires due schedulers without human action. A due scheduler SHALL be claimed atomically before firing so that overlapping ticks or multiple server instances against one database cannot fire the same occurrence twice. Claiming a recurring scheduler SHALL record its next occurrence at claim time, in the future. A scheduler whose previous run is still executing SHALL NOT be fired again by the loop or by run-now until that run reaches a terminal outcome.

#### Scenario: Concurrent claims do not double-fire
- **WHEN** two server processes claim due schedulers at the same instant and a scheduler is due
- **THEN** exactly one process claims it and one run starts

#### Scenario: Run-now while in flight is rejected
- **WHEN** run-now is requested for a scheduler whose previous run has not reached a terminal outcome
- **THEN** the response is 409 conflict and no second run starts

### Requirement: Overdue catch-up without replay
When the server starts or resumes after schedulers were overdue, a recurring scheduler SHALL fire at most once for the overdue period and its next occurrence SHALL be scheduled in the future — missed occurrences are never replayed back-to-back. A one-shot scheduler found overdue beyond a grace window SHALL be marked `missed` and archived rather than fired.

#### Scenario: Server down across a nightly fire
- **WHEN** the server was stopped at a scheduler's fire time for several hours and restarts
- **THEN** the scheduler fires once on startup recovery and its next fire time is the next future occurrence, not several fires in a burst

#### Scenario: Stale one-shot is not fired late
- **WHEN** a one-shot reminder's target instant passed more than the grace window ago while the server was down
- **THEN** the scheduler is marked `missed` and no run executes

### Requirement: Preflight before token spend
Before dispatching a scheduler run, the system SHALL resolve the acting identity and validate that the bound agent's configuration can produce a run. A scheduler whose creator is disabled or removed from the workspace SHALL be paused automatically and recorded as `blocked` with no run executed. A scheduler whose agent config, provider, or model cannot be resolved SHALL record a `blocked` run outcome without calling the model.

#### Scenario: Creator disabled pauses the scheduler
- **WHEN** the creating user is disabled and the scheduler becomes due
- **THEN** the scheduler is paused, its last run reads `blocked`, and no model call occurs

#### Scenario: Broken agent config blocks cheaply
- **WHEN** the bound agent references an unavailable provider and the scheduler fires
- **THEN** the run outcome is `blocked` with the resolution error and no tokens are spent

### Requirement: Run execution identity and sessions
A scheduler run SHALL execute under the creating user's identity resolved at fire time (role permissions, workspace context). Each run SHALL execute in its own fresh session with run origin `scheduler`, and the run's transcript SHALL be persisted as session events. Run sessions SHALL NOT appear in the per-user chat session index; each scheduler SHALL expose its runs as a list carrying status, start time, duration, and token usage, and each run SHALL be readable as a transcript.

#### Scenario: One run one session
- **WHEN** a recurring scheduler fires twice in succession
- **THEN** each fire produces its own session whose transcript contains the task prompt as the user turn and the agent's reply, and no session is reused between runs

#### Scenario: Runs are listed per scheduler
- **WHEN** a user opens a scheduler's runs
- **THEN** past runs are listed newest-first with status, start time, duration, and token usage, and each can be opened to its transcript

#### Scenario: Run sessions stay out of chat history
- **WHEN** a scheduler run completes and the creating user opens their chat sidebar for that agent
- **THEN** the run's session is absent from the list

### Requirement: Delivery of run results
A scheduler's delivery target SHALL be either `thread` (the default — the result remains in the run's own transcript) or a workspace channel, in which case the run's final reply SHALL be posted into that channel through the normal channel pipeline as an agent message. A final reply consisting solely of the token `NO_REPLY` (whole-word, case-insensitive) SHALL suppress channel delivery while the run still records as completed. A channel delivery failure SHALL be recorded on the run without reclassifying the run as failed.

#### Scenario: Channel delivery
- **WHEN** a scheduler targeting channel `#ops` completes with a final reply
- **THEN** the reply appears in `#ops` as a message authored by the agent, attributable to the scheduler

#### Scenario: Nothing to report suppresses delivery
- **WHEN** a channel-targeted run's final reply is exactly `NO_REPLY`
- **THEN** nothing is posted to the channel and the run records as completed with suppressed delivery

#### Scenario: Delivery failure is not a run failure
- **WHEN** a channel-targeted run completes but the channel post cannot be delivered (for example the agent is no longer a channel member)
- **THEN** the run records `completed` with a delivery-failure outcome, and the result remains readable in the run's transcript

### Requirement: Manual run-now
A user with `scheduler.write` SHALL be able to trigger a scheduler immediately regardless of its enabled state; running a paused scheduler SHALL NOT re-enable it. A manual run SHALL be recorded with trigger `manual`, distinguishable from scheduled fires.

#### Scenario: Run a paused schedule
- **WHEN** run-now is triggered on a paused scheduler
- **THEN** one run executes and the scheduler remains paused afterwards

### Requirement: Scheduler lifecycle mutations
Updating a scheduler's schedule SHALL recompute the next fire time from the current time under the new schedule. Disabling SHALL stop fires while retaining expression, prompt, history, and next-fire computation; enabling SHALL resume firing from the next future occurrence. Deleting a scheduler SHALL remove it and stop future fires; deleting its agent SHALL remove the scheduler with it.

#### Scenario: Edit reschedules from now
- **WHEN** a daily-09:00 scheduler is edited at 09:30 to fire daily at 10:00
- **THEN** the next fire time is today 10:00, not tomorrow 09:00

#### Scenario: Pause retains everything
- **WHEN** a scheduler is disabled and later re-enabled
- **THEN** its expression, prompt, delivery target, and run history are unchanged and firing resumes at the next future occurrence

### Requirement: Schedule agent tool
Registered agents SHALL expose a `schedule` tool with actions to create, list, update, and delete schedulers in the workspace, applying the same validation as the API — including that a channel delivery target names a channel the agent is a member of, and that one-shot creation carries a resolvable future instant. The tool SHALL NOT be available inside scheduler runs.

#### Scenario: Agent creates a channel scheduler from chat
- **WHEN** an agent's run invokes the `schedule` tool to create a daily scheduler targeting a channel it belongs to
- **THEN** the scheduler is created with that agent as its bound agent and the tool result names the schedule and its next fire time

#### Scenario: Non-member channel target rejected
- **WHEN** the `schedule` tool is invoked with a channel target the agent does not belong to
- **THEN** creation fails with a validation error naming the channel, and nothing is created

#### Scenario: Unavailable inside scheduled runs
- **WHEN** a scheduler run's toolset is resolved
- **THEN** the `schedule` tool is absent regardless of the agent's allowlist
