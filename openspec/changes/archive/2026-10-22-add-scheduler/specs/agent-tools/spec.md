# Spec Delta

## ADDED Requirements

### Requirement: Scheduler tool in the catalog

The built-in tool catalog SHALL include a `scheduler` tool (display name "Scheduler") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The agent create/edit allowlist chips and the Tools pane card list SHALL list it without bespoke wiring.

#### Scenario: Scheduler appears in the catalog surfaces

- **WHEN** the Tools pane or the agent tool allowlist is rendered
- **THEN** a "Scheduler" entry is listed alongside the other built-in tools

#### Scenario: Disabled or unallowlisted scheduler is not offered

- **WHEN** the scheduler tool is disabled in the Tools pane, or an agent's allowlist omits it
- **THEN** that agent's turns are offered no `scheduler` tool

### Requirement: Scheduler actions

The `scheduler` tool SHALL expose a single `action` argument with the values `create`, `list`, `update`, and `delete`, taking only scalar arguments: `id`, `name`, `agent` (an agent name or id), `prompt`, `cron` (a five-field expression in local time), and `enabled`. `create` SHALL apply the same validation as the Schedules editor, SHALL default `agent` to the agent making the call, SHALL default `enabled` to true, and SHALL return the created schedule's id, human-readable recurrence, and next fire time. `list` SHALL return every schedule with id, name, agent, expression, human-readable recurrence, enabled flag, next fire time, and last run. `update` SHALL change only the supplied fields of the schedule identified by `id`. `delete` SHALL remove the schedule identified by `id`. Validation failures and unknown ids SHALL be returned as readable tool errors rather than failing the turn, and every change made through the tool SHALL be visible in the Schedules menu immediately.

#### Scenario: Agent creates a schedule for itself from chat

- **WHEN** the user asks the agent to check a URL every weekday morning and the model calls `scheduler` with `action: create`, a `name`, a `prompt`, and `cron: 0 9 * * 1-5`, omitting `agent`
- **THEN** a schedule for the calling agent is created and enabled, the result reports its id, the recurrence described as weekdays at 09:00, and the next fire time, and the schedule is listed in the Schedules menu

#### Scenario: Invalid expression is a readable error

- **WHEN** the model calls `action: create` with `cron: every monday`
- **THEN** the tool returns an error result explaining the expression is invalid and showing the expected five-field form, nothing is created, and the turn continues

#### Scenario: Duplicate name points at update

- **WHEN** the model calls `action: create` with a name that an existing schedule already uses
- **THEN** the tool returns an error result naming the existing schedule's id and suggesting `update`

#### Scenario: List reports schedule state

- **WHEN** the model calls `action: list`
- **THEN** the result carries every schedule with its id, name, agent, expression, human-readable recurrence, enabled flag, next fire time, and last run outcome

#### Scenario: Update disables by id

- **WHEN** the model calls `action: update` with an existing `id` and `enabled: false`
- **THEN** the schedule is disabled, its other fields are unchanged, and the Schedules menu shows it off

#### Scenario: Delete of an unknown id is a readable error

- **WHEN** the model calls `action: delete` with an id that does not exist
- **THEN** the tool returns an error result saying so and the turn continues
