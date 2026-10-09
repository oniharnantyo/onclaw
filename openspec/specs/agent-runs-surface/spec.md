# agent-runs-surface Specification

## Purpose

User-facing visibility and control over a session's background tasks — delegations and background shells — through a session-scoped tasks API: list what ran and what is running, watch a task's trajectory live, replay an ended task's trajectory from its persisted output file, and cancel a running background task. The parent transcript stays exactly one card per delegation; this surface is additive and separately addressable.

## Requirements

### Requirement: Session tasks list
The backend SHALL expose a member-facing endpoint listing a session's background tasks. Each entry SHALL carry the task id, the lane kind (`delegation` or `shell`), a status (`running`, `completed`, `failed`, or `canceled`), the task description (the one-line human summary), and the output file location when one was reserved. The list SHALL merge the live run's task space with the session's persisted task-created and task-completed session events, so tasks from earlier turns and from ended runs appear with their persisted outcome. Foreground delegations never become tasks and SHALL NOT appear. Access SHALL follow the session's own access rules with workspace scoping enforced at the data layer.

#### Scenario: Live and historical tasks in one list
- **WHEN** a session has one running background delegation, one completed shell task from an earlier turn, and one canceled delegation from a previous run
- **THEN** the list shows all three with their respective statuses, kinds, descriptions, and output locations

#### Scenario: Foreground delegation absent
- **WHEN** a session's current turn runs a foreground delegation (no run_in_background)
- **THEN** no task entry appears for it, before or after it finishes

### Requirement: Live task progress stream
The backend SHALL expose a dedicated SSE endpoint streaming the session's running tasks' progress: task-keyed frames carrying the child execution records — text messages, tool calls, and tool results — as the child produces them, for delegation and shell tasks alike. Frames SHALL be addressable by task id, and the stream SHALL deliver a terminal frame when a task reaches a final status. The stream SHALL be live-only: progress records SHALL NOT be persisted as session events, SHALL NOT enter the parent session's transcript or context, and SHALL NOT be delivered on the turn stream or the session-events stream. A client that never opens the stream SHALL observe no behavioral difference.

#### Scenario: Live trajectory while a delegation runs
- **WHEN** a background delegation executes tool calls and text while a client is subscribed to the session's task stream
- **THEN** task-keyed frames carrying those records arrive as they are produced, and the parent transcript still carries exactly one delegation tool call

#### Scenario: Isolation from the parent transcript
- **WHEN** any task produces progress records
- **THEN** no progress record is persisted onto the session's events, and reloading the transcript shows exactly what it showed before the surface existed

#### Scenario: Terminal frame on completion
- **WHEN** a subscribed running task reaches a terminal status
- **THEN** the stream delivers a terminal frame naming the task, its outcome, and its output location, then stops sending frames for that task

### Requirement: Task trajectory read
The backend SHALL expose an endpoint reading one task's trajectory. For a running task it SHALL return the progress records produced so far, optionally from a client-supplied cursor. For an ended task it SHALL replay the persisted output file from the task's recorded output location. When the output file no longer exists, the endpoint SHALL return a clear not-found rather than an error masquerading as an empty trajectory.

#### Scenario: Ended task replays from disk
- **WHEN** a client reads the trajectory of a delegation that finished in a previous run
- **THEN** the response contains the child's recorded messages and tool calls from the persisted output file

#### Scenario: Pruned output reads as not-found
- **WHEN** a task's output file has been removed while its persisted completion event remains
- **THEN** the trajectory endpoint returns not-found naming the task, and the list still shows the task with its outcome and summary

### Requirement: Task cancel
The backend SHALL expose an endpoint canceling one running background task, addressed by task id through the run's task space — the same control surface the model's `task_stop` tool uses. Only tasks in a running status SHALL be cancellable; canceling SHALL return the task's cancellation acceptance, and the task SHALL reach the `canceled` outcome and surface a completion event exactly as a model-initiated stop does. Canceling a task that is not running SHALL be refused with a conflict error. Capability-wise the endpoint SHALL be reachable for any session whose task list is reachable, without requiring the agent to expose any additional tool.

#### Scenario: Cancel a running background delegation
- **WHEN** a client cancels a running background delegation by task id
- **THEN** the delegation terminates with the canceled outcome, a completion event surfaces in the parent session, and the trajectory shows the canceled boundary

#### Scenario: Cancel refused for ended task
- **WHEN** a client cancels a task that already completed, failed, or was canceled
- **THEN** the endpoint responds with a conflict error and no state changes

### Requirement: Process-local honesty
Task visibility SHALL be honest about lifetime: running tasks exist only while their run lives, the list SHALL NOT fabricate entries for tasks lost to a restart mid-run, and a task whose run died without reaching a terminal status SHALL NOT be presented as running by the merged list — persisted evidence only claims what it recorded.

#### Scenario: Restart mid-run leaves no ghost
- **WHEN** the server restarts while a background delegation runs and the session is later listed
- **THEN** the task appears only as its persisted evidence shows (created, without a completion), never as running
