## MODIFIED Requirements

### Requirement: Structured agent configuration
Deploying a new agent or editing an existing one SHALL happen through a three-step wizard modal. **Step 1 (Identity)** SHALL expose exactly one labeled control per property — name, slug (auto-suggested from name, editable), role (short, free-form with kebab-case suggestions), description (a short summary shown on the roster card), goal & behavior brief (textarea, the generation driver), and avatar picker (react-nice-avatar prop controls with randomize). **Step 2 (Model)** SHALL expose provider select (workspace's configured provider configs, API-driven), model combobox (dropdown fed by the model-catalog endpoints, free-text when the catalog returns nothing) with its effort dropdown, and a collapsed "Advanced model configuration" section holding temperature and max_tokens only. **Step 3 (Capabilities, skippable)** SHALL expose tool toggles, skill toggles (workspace skills), MCP server toggles, and the autonomy selector (`approval | suggest | full`) accompanied by a short description of the selected autonomy. One labeled control per property; raw JSON editing MUST NOT be offered for any structured value. Save/create SHALL be disabled until required fields are valid; the collapsed Advanced section SHALL auto-expand on submit if its fields are invalid. Step 3 toggles list only capabilities that exist (existing tools are registry names, skills from `GET /skills`, MCP servers from workspace settings; registries for tools/MCP validation come later).

**The Prompts tab (edit mode)** SHALL present the generated documents as a two-pane view — a file list on the left (IDENTITY.md, SOUL.md, BOOTSTRAP.md) and a preview of the selected file on the right — together with the prompts status and the Regenerate action; identity and soul previews are editable monospace textareas persisted via the PATCH contract on Save changes, and the bootstrap preview is read-only. Activating Regenerate SHALL ask the user what should change before running: submitting the change-request form enhances the existing documents with the requested change (the old prompts are always included in the model call), and submitting it empty enhances without an instruction. After a successful regeneration the modal SHALL stay open and refetch the agent so the enhanced documents are visible immediately. The tab SHALL note that regeneration keeps the previous version of each file beside it as `<name>.bak`.

#### Scenario: Deploy a new agent
- **WHEN** the user completes all three wizard steps and confirms
- **THEN** the wizard shows its loading state while the create request generates prompts, and the roster includes the new agent with `prompts_status: ready` (generation gates persistence — see agent-prompts)

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

#### Scenario: Edit hydrates from detail
- **WHEN** the user opens the config modal to edit an existing agent
- **THEN** the form is populated from the agent detail response, including the server-side generated prompt documents; saving without edits leaves those documents unchanged

#### Scenario: Prompts tab lists the generated files
- **WHEN** the config modal opens an existing agent on the Prompts tab
- **THEN** IDENTITY.md, SOUL.md, and BOOTSTRAP.md are listed with the prompts status and Regenerate action; identity and soul are editable textareas persisted on Save changes, and the bootstrap document is read-only

#### Scenario: Prompts tab previews the selected file
- **WHEN** the user selects a file in the left-hand list
- **THEN** the right pane previews exactly that file — identity/soul editable, bootstrap read-only

#### Scenario: Regenerate asks what should change
- **WHEN** Regenerate is activated
- **THEN** a change-request form opens; submitting it with a change enhances the existing documents with that change applied, and submitting it empty enhances without an instruction

#### Scenario: Regenerate refreshes the tab
- **WHEN** regeneration completes from the Prompts tab
- **THEN** the modal stays open, refetches the agent, and shows the enhanced documents with status ready
