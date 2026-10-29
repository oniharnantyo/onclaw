# Spec Delta

## MODIFIED Requirements

### Requirement: Agent list layout

The agents menu SHALL present a master-detail layout: an agent list column
and a detail column for the selected agent. Each list entry SHALL show the
agent's generated avatar, its name, its configured model,
and a prompt-generation status indicator while documents are generating or
failed, with a retry affordance on failure. Selecting an entry SHALL show that
agent's configuration in the detail column — the full configuration editor
(see "Edit agent configuration") with its section navigation — without a
modal dialog; configuration changes MUST reflect back on the list entry. A
chat affordance per entry MUST navigate to that agent's chat. With no agents,
the menu SHALL show the create empty state.

#### Scenario: Entry reflects configuration

- **WHEN** an agent's model or description is changed in its configuration
- **THEN** its list entry shows the updated model

#### Scenario: Entry opens chat

- **WHEN** the user activates an agent list entry's chat affordance
- **THEN** the chat menu opens that agent's most recent session

#### Scenario: Failed generation shows retry on the entry

- **WHEN** an agent's prompt generation has failed
- **THEN** the entry shows the failed indicator with a retry affordance

#### Scenario: Selecting an agent shows its configuration

- **WHEN** the user selects an agent in the list column
- **THEN** the detail column shows that agent's configuration editor and no
  modal dialog is involved
