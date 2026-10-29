# Spec Delta

## ADDED Requirements

### Requirement: Agent workspace skills management

An agent's configuration SHALL list the skills in that agent's own workspace
skills directory, each with its name and description, alongside the skill
allowlist. Each entry SHALL offer a Promote action that copies the skill into
the global skills library and a Delete action that removes the workspace
skill. The outcome of a promotion SHALL be displayed: on success the skill is
registered in the global library; on a name collision the rejection reason is
shown and nothing changes. The section SHALL render legibly in both themes.

#### Scenario: Personal skills are listed

- **WHEN** the user opens an agent's configuration and the agent's workspace
  skills directory contains skills
- **THEN** each skill is listed with its name and description next to the
  skill allowlist

#### Scenario: Promote reports success

- **WHEN** the user promotes a workspace skill whose name is free in the
  global library
- **THEN** the outcome is shown as success and the skill appears in the
  Skills pane as a registered library skill

#### Scenario: Promote reports the collision

- **WHEN** the user promotes a workspace skill whose name already exists in
  the global library
- **THEN** the rejection reason is shown and neither the workspace skill nor
  the library skill changed

#### Scenario: Delete removes the entry

- **WHEN** the user deletes a workspace skill from the list
- **THEN** the entry disappears from the list and the agent's next turn no
  longer sees that skill
