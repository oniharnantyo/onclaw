## MODIFIED Requirements

### Requirement: Structured agent configuration
Deploying a new agent or editing an existing one SHALL happen through a three-step wizard modal. **Step 1 (Identity)** SHALL expose exactly one labeled control per property — name, slug (auto-suggested from name, editable), role (short, free-form with kebab-case suggestions), description (textarea), goal & behavior brief (textarea, the generation driver), and avatar picker (react-nice-avatar prop controls with randomize). **Step 2 (Model)** SHALL expose provider select (workspace's configured provider configs, API-driven), model combobox (dropdown fed by the model-catalog endpoints, free-text when the catalog returns nothing) with its effort dropdown, and a collapsed "Advanced model configuration" section holding temperature and max_tokens only. **Step 3 (Capabilities, skippable)** SHALL expose tool toggles, skill toggles (workspace skills), MCP server toggles, and the autonomy selector (`approval | suggest | full`) accompanied by a short description of the selected autonomy. One labeled control per property; raw JSON editing MUST NOT be offered for any structured value. Save/create SHALL be disabled until required fields are valid; the collapsed Advanced section SHALL auto-expand on submit if its fields are invalid. Step 3 toggles list only capabilities that exist (existing tools are registry names, skills from `GET /skills`, MCP servers from workspace settings; registries for tools/MCP validation come later).

#### Scenario: Deploy a new agent
- **WHEN** the user completes all three wizard steps and confirms
- **THEN** the agent is created (`prompts_status: generating`), the UI shows the generating state, and the roster includes it

#### Scenario: Step 3 skippable
- **WHEN** the user deploys without selecting any capability chip
- **THEN** the agent is created with empty tools/skills/mcp arrays

#### Scenario: Provider cascade
- **WHEN** the user switches provider while a model of the first is selected
- **THEN** the model selection resets to the new provider's first model (or empty when the catalog returns none)

#### Scenario: Skill options include workspace skills
- **WHEN** the workspace has custom skills
- **THEN** they appear as toggleable options in Step 3 alongside registry skills

#### Scenario: Advanced section auto-expand
- **WHEN** continue is pressed on Step 2 with an invalid temperature/max_tokens in the collapsed section
- **THEN** the section auto-expands and shows the field errors

#### Scenario: Unconfigured type entry
- **WHEN** an agent is configured while "Gemini" has no workspace provider config
- **THEN** Gemini appears as a disabled option hinting at Settings → Providers (or is omitted)

## ADDED Requirements

### Requirement: Prompt generation status display
Agent surfaces (roster cards, detail, chat header) SHALL show prompt-generation state: `generating` as an animated indicator, `ready` silently, and `failed` with the short `prompts_error` plus a Retry action (regenerate endpoint). Generation runs synchronously inside create/regenerate — the wizard and birth flow show the interactive loading experience while it runs (see agent-prompts), and roster states cover the residual in-flight and failed cases.

#### Scenario: Generating indicator on the roster
- **WHEN** an agent shows prompts_status generating
- **THEN** its card shows an animated "generating prompts…" indicator

#### Scenario: Failed with retry
- **WHEN** generation failed with "provider rejected the key"
- **THEN** the card/detail shows the error text and a Retry button calling regenerate

### Requirement: Model and effort selection
Model SHALL be a combobox: options from `GET /workspaces/:ws/providers/:id/models` when a provider is selected (or `models-preview` during onboarding birth), free-text entry when the source is `none` or the model is not listed. Effort dropdown SHALL list the selected model's resolved efforts; hidden when the list is empty. Provider/model are main-form (visible) required fields — provider/model MUST NOT be inside the collapsed Advanced section.

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
