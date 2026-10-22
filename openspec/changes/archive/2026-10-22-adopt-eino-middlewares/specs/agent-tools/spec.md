# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: Todo tools`
- TO: `### Requirement: Task plan tools`

## MODIFIED Requirements

### Requirement: Task plan tools

The `task_create`, `task_get`, `task_update`, and `task_list` tools SHALL let the agent maintain a structured task plan that persists in the agent's workspace across turns. The four tools SHALL be gated through the turn's tool gate exactly like every other built-in tool: each is offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist, and the Tools pane card list and the agent create/edit allowlist chips SHALL list them in place of the retired todo pair. Plans written in the retired todo format are not migrated; the plan starts empty.

#### Scenario: Plan persists across turns

- **WHEN** the agent creates tasks in one turn and lists them in a later turn
- **THEN** the same tasks are returned

#### Scenario: Task lifecycle transitions

- **WHEN** the agent creates a task, moves it in progress, and completes it
- **THEN** task_list reflects each transition

#### Scenario: Gated through the tool gate

- **WHEN** the task tools are disabled in the Tools pane, or an agent's allowlist omits them
- **THEN** that agent's turns are offered none of the four tools
