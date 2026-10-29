# agent-management Specification

## Purpose

Provide the agents menu: a card grid for browsing agents and full agent configuration (provider/model, system prompt, skills, tools, MCP servers, hooks) ported from onclaw-v2's agent editor, using generated avatars from react-nice-avatar.

## Requirements

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

### Requirement: Edit agent configuration

Agent configuration SHALL include, at minimum: provider and model selection; an optional default reasoning effort whose options are limited to those the selected model supports, reset when a model without effort support is chosen; the agent's three prompt documents (IDENTITY, SOUL, BOOTSTRAP) in editable multi-line editors presented behind a file rail, a regenerate control for the documents, enabled skills, enabled tools, MCP server selection (every enabled server on by default), per-agent hook assignment with ordering, and a subagents capability toggle. Changes MUST be saved atomically and reflected in subsequent turns without requiring an app restart. The configured effort is the default effort for the agent's sessions whenever a session carries no effort override.

#### Scenario: Model change applies to next turn

- **WHEN** the user switches an agent's model and sends a message in its chat
- **THEN** the turn executes on the newly selected model

#### Scenario: Configuration sections

- **WHEN** the user opens an agent's configuration
- **THEN** the provider/model with its effort default, prompt documents with regenerate, skills, tools, MCP, hooks, and subagents sections are all present and editable

#### Scenario: Document editor reflects the workspace file

- **WHEN** the user opens an agent's prompt document editors
- **THEN** each editor shows the current content of the corresponding file in the agent's workspace directory and saving rewrites that file

#### Scenario: Assigned hooks apply to the agent's turns

- **WHEN** the user assigns a hook to an agent and sends a message in its chat
- **THEN** the hook is evaluated on that turn

#### Scenario: Subagent toggle takes effect

- **WHEN** the user disables subagents for an agent
- **THEN** its subsequent turns are offered no agent tool

#### Scenario: Effort options follow the model

- **WHEN** the user selects a reasoning-capable model in the Model section
- **THEN** the effort control offers only that model's supported options, and switching to a model without effort support clears the saved effort

#### Scenario: Agent effort is the session default

- **WHEN** a new chat session starts for an agent with a configured default effort and no session override
- **THEN** the first turn applies the agent's default effort

### Requirement: Delete agent

The user SHALL be able to delete an agent. Deleting an agent MUST require
confirmation and MUST NOT delete the agent's past session history.

#### Scenario: Delete with confirmation

- **WHEN** the user confirms deletion of an agent
- **THEN** the agent is removed from the agent list while its sessions remain
  in the data store

### Requirement: Theme parity

The agents menu SHALL render correctly in both the light and dark app themes, including cards, avatars, and all configuration sections.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while viewing the agents menu
- **THEN** all cards and configuration sections render legibly in the selected theme

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
