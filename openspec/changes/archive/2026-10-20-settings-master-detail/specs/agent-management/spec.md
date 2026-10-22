# Spec Delta

## REMOVED Requirements

### Requirement: Agent cards

The agents menu SHALL display each agent as a card showing its generated
avatar (react-nice-avatar), name, configured model, an optional description
line (clamped to the card), and enabled state. A card SHALL show a
prompt-generation status indicator while documents are generating or failed,
with a retry affordance on failure. Selecting a card MUST navigate to that
agent's chat; an edit affordance MUST open the agent's configuration.

#### Scenario: Card reflects configuration

- **WHEN** an agent's model or description is changed in its configuration
- **THEN** its card shows the updated model and description

#### Scenario: Card opens chat

- **WHEN** the user selects an agent card's chat affordance
- **THEN** the chat menu opens that agent's most recent session

#### Scenario: Failed generation shows retry on the card

- **WHEN** an agent's prompt generation has failed
- **THEN** the card shows the failed indicator with a retry affordance

**Reason**: the card grid hides configuration behind a modal; the reference UX
edits agents in place with a master-detail layout.

**Migration**: replaced by "Agent list layout", which carries over the entry
content (avatar, name, status, retry) and both affordances (chat, configure)
in the master-detail shape.

## ADDED Requirements

### Requirement: Agent list layout

The agents menu SHALL present a master-detail layout: an agent list column
and a detail column for the selected agent. Each list entry SHALL show the
agent's generated avatar (react-nice-avatar), its name, its configured model,
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

## MODIFIED Requirements

### Requirement: Create agent

The user SHALL create a new agent through a three-step wizard — Identity,
Model, Capabilities. Creation SHALL require a name, a slug, and a
goal-and-behavior brief; description SHALL be optional and entered as
multi-line text. The slug SHALL follow DNS-label rules, be unique among
agents, be suggested automatically from the name, and be immutable after
creation. The wizard SHALL validate each step before advancing. Creating
SHALL persist the agent row with prompt status `generating` and complete only
after synchronous prompt generation, showing a rotating informal onboarding
loader while in flight. The Identity step SHALL present an AvatarPicker — a
procedural-avatar preview with Randomize and a Customize trait grid — whose
full config the wizard submits and the agent row stores verbatim; avatar
surfaces SHALL render the stored config when present and fall back to the
deterministic id-derived avatar when the row has none. The agent SHALL be
immediately usable in chat once a provider and model are selected in the
Model step.

#### Scenario: Create minimal agent

- **WHEN** the user creates an agent named "Scout" with a suggested slug and
  a one-sentence brief, accepting the defaults elsewhere
- **THEN** the agent appears in the agent list with its own generated avatar
  and can be configured

#### Scenario: Wizard collects identity in three steps

- **WHEN** the user advances through Identity, Model, and Capabilities and
  submits
- **THEN** the agent is created with the entered name, slug, description,
  brief, provider/model, and capability allowlists

#### Scenario: Slug is suggested, validated, and immutable

- **WHEN** the user types the name "Research Scout", edits the suggested slug
  to an invalid value, or reuses another agent's slug
- **THEN** "research-scout" is suggested from the name, invalid and duplicate
  slugs are rejected at the step, and the slug cannot be changed in the
  configuration editor

#### Scenario: Brief is required

- **WHEN** the user tries to advance past the Identity step without a brief
- **THEN** the step shows a validation error explaining that the brief drives
  prompt generation

#### Scenario: Create shows the onboarding loader

- **WHEN** the user submits the wizard and generation is in flight
- **THEN** the wizard shows the onboarding loader with rotating informal
  status lines until the create call returns

#### Scenario: Failed generation keeps the wizard's agent

- **WHEN** prompt generation fails during create
- **THEN** the agent appears in the agent list with the failed indicator and
  stays editable

### Requirement: Delete agent

The user SHALL be able to delete an agent. Deleting an agent MUST require
confirmation and MUST NOT delete the agent's past session history.

#### Scenario: Delete with confirmation

- **WHEN** the user confirms deletion of an agent
- **THEN** the agent is removed from the agent list while its sessions remain
  in the data store
