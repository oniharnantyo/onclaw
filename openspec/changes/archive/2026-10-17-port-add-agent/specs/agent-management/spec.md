# Spec Delta

## MODIFIED Requirements

### Requirement: Agent cards

The agents menu SHALL display each agent as a card showing its generated avatar (react-nice-avatar), name, configured model, an optional description line (clamped to the card), and enabled state. A card SHALL show a prompt-generation status indicator while documents are generating or failed, with a retry affordance on failure. Selecting a card MUST navigate to that agent's chat; an edit affordance MUST open the agent's configuration.

#### Scenario: Card reflects configuration

- **WHEN** an agent's model or description is changed in its configuration
- **THEN** its card shows the updated model and description

#### Scenario: Card opens chat

- **WHEN** the user selects an agent card's chat affordance
- **THEN** the chat menu opens that agent's most recent session

#### Scenario: Failed generation shows retry on the card

- **WHEN** an agent's prompt generation has failed
- **THEN** the card shows the failed indicator with a retry affordance

### Requirement: Create agent

The user SHALL create a new agent through a three-step wizard — Identity, Model, Capabilities. Creation SHALL require a name, a slug, and a goal-and-behavior brief; description SHALL be optional and entered as multi-line text. The slug SHALL follow DNS-label rules, be unique among agents, be suggested automatically from the name, and be immutable after creation. The wizard SHALL validate each step before advancing. Creating SHALL persist the agent row with prompt status `generating` and complete only after synchronous prompt generation, showing a rotating informal onboarding loader while in flight. The Identity step SHALL present an AvatarPicker — a procedural-avatar preview with Randomize and a Customize trait grid — whose full config the wizard submits and the agent row stores verbatim; avatar surfaces SHALL render the stored config when present and fall back to the deterministic id-derived avatar when the row has none. The agent SHALL be immediately usable in chat once a provider and model are selected in the Model step.

#### Scenario: Create minimal agent

- **WHEN** the user creates an agent named "Scout" with a suggested slug and a one-sentence brief, accepting the defaults elsewhere
- **THEN** the agent appears in the cards grid with its own generated avatar and can be configured

#### Scenario: Wizard collects identity in three steps

- **WHEN** the user advances through Identity, Model, and Capabilities and submits
- **THEN** the agent is created with the entered name, slug, description, brief, provider/model, and capability allowlists

#### Scenario: Slug is suggested, validated, and immutable

- **WHEN** the user types the name "Research Scout", edits the suggested slug to an invalid value, or reuses another agent's slug
- **THEN** "research-scout" is suggested from the name, invalid and duplicate slugs are rejected at the step, and the slug cannot be changed in the configuration editor

#### Scenario: Brief is required

- **WHEN** the user tries to advance past the Identity step without a brief
- **THEN** the step shows a validation error explaining that the brief drives prompt generation

#### Scenario: Create shows the onboarding loader

- **WHEN** the user submits the wizard and generation is in flight
- **THEN** the wizard shows the onboarding loader with rotating informal status lines until the create call returns

#### Scenario: Failed generation keeps the wizard's agent

- **WHEN** prompt generation fails during create
- **THEN** the agent appears in the grid with the failed indicator and stays editable

### Requirement: Edit agent configuration

Agent configuration SHALL include, at minimum: provider and model selection, the agent's three prompt documents (IDENTITY, SOUL, BOOTSTRAP) in editable multi-line editors presented behind a file rail, a regenerate control for the documents, enabled skills, enabled tools, MCP server opt-ins, and hooks. Changes MUST be saved atomically and reflected in subsequent turns without requiring an app restart.

#### Scenario: Model change applies to next turn

- **WHEN** the user switches an agent's model and sends a message in its chat
- **THEN** the turn executes on the newly selected model

#### Scenario: Configuration sections

- **WHEN** the user opens an agent's configuration
- **THEN** the provider/model, prompt documents with regenerate, skills, tools, MCP, and hooks sections are all present and editable

#### Scenario: Document editor reflects the workspace file

- **WHEN** the user opens an agent's prompt document editors
- **THEN** each editor shows the current content of the corresponding file in the agent's workspace directory and saving rewrites that file
