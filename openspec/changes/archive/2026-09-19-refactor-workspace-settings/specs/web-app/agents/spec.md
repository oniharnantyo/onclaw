## MODIFIED Requirements

### Requirement: Model and effort selection
Model SHALL be a combobox: options from `GET /workspaces/:ws/providers/:id/models` when a provider is selected (or `models-preview` during onboarding birth), free-text entry when the source is `none` or the model is not listed. Effort dropdown SHALL list the selected model's resolved efforts; hidden when the list is empty. The provider select SHALL additionally offer a "Workspace default (inherit)" option whenever the workspace has a default model: selecting it stores the empty provider/model pair (inheritance), hides the model combobox and effort dropdown, and hides the workspace-defaults-dependent save errors — the option SHALL NOT render when no default model exists. Provider/model are main-form (visible) required fields on the pinned path — provider/model MUST NOT be inside the collapsed Advanced section.

#### Scenario: Dropdown from live source
- **WHEN** a provider is selected and the live models API answers
- **THEN** the model combobox lists the provider's models

#### Scenario: Free-text fallback
- **WHEN** the models response is `source: "none"`
- **THEN** the combobox accepts free-text model ids

#### Scenario: Effort dropdown hidden
- **WHEN** a model with no effort values is selected
- **THEN** the effort dropdown is hidden

#### Scenario: Provider/model outside Advanced
- **WHEN** the wizard renders Step 2
- **THEN** provider and model are visible main-form fields, not inside Advanced

#### Scenario: Inherit option when a default exists
- **WHEN** the workspace default model is set and the wizard renders Step 2
- **THEN** the provider select offers "Workspace default (inherit)"

#### Scenario: Selecting inherit hides the model picker
- **WHEN** the user selects "Workspace default (inherit)"
- **THEN** the model combobox and effort dropdown are hidden and the agent saves with the empty pair

#### Scenario: Inherit option absent without a default
- **WHEN** no workspace default model is set and the wizard renders Step 2
- **THEN** the provider select offers only the workspace's configured providers and a pinned pair is required
