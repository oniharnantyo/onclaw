## ADDED Requirements

### Requirement: Floating run card
The direct-chat view SHALL render a floating run card over the transcript — a dismissible card anchored inside the chat pane's top corner, with a full-height bottom-sheet fallback on narrow viewports — whenever the session has run activity: a todo plan (agent-todos Progress section) or background-task activity (the Agents section below). The card SHALL NOT occupy the chat header; the header stays clean. The card auto-surfaces once per run on the first todo write or background-task activity, and dismissal SHALL be sticky for the remainder of the run (manual dismissal keeps it closed; a new run arms it again). When the session has neither todo plans nor background-task activity, no card renders anywhere (present-only).

#### Scenario: Auto-surface on first activity
- **WHEN** a running session's agent writes a todo plan or its first delegation starts
- **THEN** the floating card appears over the transcript without user action, exactly once for the run

#### Scenario: Sticky dismissal
- **WHEN** the user dismisses the card and the run continues producing todos and agent activity
- **THEN** the card stays closed until the next run begins

#### Scenario: Clean header and absent card
- **WHEN** the session has no todo plan and no background-task activity
- **THEN** the chat header carries no run-status chip and no card renders

### Requirement: Agents section
The floating run card SHALL host an Agents section listing the session's background tasks — delegations and shell tasks alike — with one row per task carrying the subagent name (or the shell command excerpt for shell tasks), the task description, and a live status marker. Running tasks SHALL render with a running marker; ended tasks SHALL collapse into a single Ended summary row rendered in the completed state — a check-circle marker, the Ended label, the ended count aligned opposite the label, and a chevron affordance — placed inside the Agents section as its closing row beneath the running rows, never as a sibling section. Activating the section header or any row SHALL open the right panel's Subagents source (list view; the row's task pre-selected for detail when addressable). Task identity joins to the transcript's `agent` tool cards by description (order as tiebreak) — a display-only join.

#### Scenario: Running rows with live status
- **WHEN** a session has two running delegations and one running background shell
- **THEN** the section lists all three with running markers, names, and descriptions

#### Scenario: Ended summary opens the panel
- **WHEN** the user activates the "Ended · 4 ›" row
- **THEN** the right panel opens on the Subagents list view showing the session's ended tasks

### Requirement: Subagents panel source
The right panel SHALL offer a Subagents source with two views: a list view sectioned Running / Ended — running rows with name, kind badge, elapsed time, description, and a live preview strip of the task's latest progress records; ended rows with name, kind badge, outcome, relative time, and a two-line snippet — and a trajectory detail view per task with back navigation to the list, the task's kind, status, elapsed time, tool-call count, and the child transcript rendered as text blocks and tool-call rows. Running tasks' trajectories SHALL arrive through the session tasks progress stream; ended tasks' trajectories SHALL replay from the trajectory endpoint. The panel SHALL keep working after a reload: ended tasks remain listed and replayable from persisted evidence.

#### Scenario: List shows live preview
- **WHEN** the panel's list view is open while a delegation runs
- **THEN** its row shows a preview strip of its latest progress records, updating live

#### Scenario: Ended task replays after reload
- **WHEN** the user reopens the session later and opens an ended delegation's detail
- **THEN** the trajectory renders from the persisted output, identical in content to what the live view showed

#### Scenario: Detail navigation
- **WHEN** the user activates a list row
- **THEN** the panel swaps to that task's trajectory detail with a back control returning to the list

### Requirement: Task stop affordance
The Subagents detail view SHALL render a Stop control for the open task. The control SHALL be enabled only while the task is running AND is a cancellable background task; for foreground delegations — which are not tasks — any stop affordance SHALL be disabled and explain that only background tasks can be stopped. Activating the control SHALL call the task cancel endpoint; on success the task's status SHALL flip to canceled across the card, panel, and transcript card state without a reload. The parent run's own stop control remains the only halt for foreground delegations.

#### Scenario: Stop ends a background task
- **WHEN** the user activates Stop on a running background delegation
- **THEN** the cancel endpoint accepts, and the task shows canceled everywhere without reload

#### Scenario: Foreground shows disabled stop
- **WHEN** the detail view addresses a foreground delegation
- **THEN** the stop control renders disabled with a "background only" explanation

### Requirement: Delegation and shell card deep-links
The transcript's `agent` and `background_shell` tool cards SHALL render with their own display treatment — icon, one-liner derived from the call's description (or command), and live status — and an affordance opening the right panel's Subagents detail on the joined task. Cards without a joinable task SHALL still render the display treatment and open the list view. The cards SHALL carry no inner child events — the one-card transcript rule is unchanged; deep-links only navigate.

#### Scenario: Card deep-links to trajectory
- **WHEN** the user activates the panel affordance on a running `agent` tool card whose task is joinable
- **THEN** the right panel opens on that task's trajectory detail

#### Scenario: Unjoinable card still opens the panel
- **WHEN** the affordance is activated on a `background_shell` card whose task id cannot be joined
- **THEN** the panel opens on the Subagents list view
