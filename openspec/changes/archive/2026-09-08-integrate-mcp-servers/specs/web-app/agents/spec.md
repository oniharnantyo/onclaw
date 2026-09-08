# web-app/agents Delta

## MODIFIED Requirements

### Requirement: Structured agent configuration
Deploying a new agent or editing an existing one SHALL happen through a three-step wizard modal. **Step 1 (Identity)** SHALL expose exactly one labeled control per property — name, slug (auto-suggested from name, editable), role (short, free-form with kebab-case suggestions), description (a short summary shown on the roster card), goal & behavior brief (textarea, the generation driver), and avatar picker (react-nice-avatar prop controls with randomize). **Step 2 (Model)** SHALL expose provider select (workspace's configured provider configs, API-driven), model combobox (dropdown fed by the model-catalog endpoints, free-text when the catalog returns nothing) with its effort dropdown, and a collapsed "Advanced model configuration" section holding temperature and max_tokens only. **Step 3 (Capabilities, skippable)** SHALL expose tool toggles, the skills inventory, the MCP section, and the autonomy selector (`approval | suggest | full`) accompanied by a short description of the selected autonomy. Tool toggles SHALL render as one flat row of chips — no group headings — fed by the workspace tools endpoint (catalog merged with workspace state), showing each tool's catalog display name and icon; stored allowlist names remain the catalog keys. The browser facade SHALL appear as a single "Browser" chip (selecting it stores the `browser` alias; an agent whose allowlist carries any `browser.*` name or the alias shows the chip selected), and shell SHALL appear as a single "Shell" chip. Tools disabled at workspace level SHALL render greyed and unselectable with a tooltip naming Settings → Tools. The skills inventory SHALL show locked chips for every system skill and every enabled workspace skill — always attached, not toggleable, with a hint naming Settings → Skills — followed by an "Agent skills" section listing this agent's own skills with add (install scoped to this agent) and remove actions for `skills.write` holders. A locked chip whose skill has unmet tool dependencies SHALL render a warning naming the missing tool and pointing at the tool chips below. The MCP section SHALL list every registered workspace MCP server as a labeled row with a status hint and a toggle defaulting to OFF — an agent gains a server's tools only by opting in, and toggling a server on or off stores or removes its id in the agent's `enabled_mcps` on save; a server paused at workspace level renders with a paused hint and opting into it warns that it currently contributes nothing. The section SHALL follow with an "Agent MCP servers" sub-list showing this agent's private servers with add, edit, and remove actions (for `agents.write` holders) through the same structured transport-branched dialog used in Settings → MCP servers. One labeled control per property; raw JSON editing MUST NOT be offered for any structured value. Save/create SHALL be disabled until required fields are valid; the collapsed Advanced section SHALL auto-expand on submit if its fields are invalid. Step 3 lists only capabilities that exist (tools from the catalog endpoint, skills from the skills API, workspace MCP servers from the workspace MCP endpoints, private servers from the agent).

**The Prompts tab (edit mode)** SHALL present the generated documents as a two-pane view — a file list on the left (IDENTITY.md, SOUL.md, BOOTSTRAP.md) and a preview of the selected file on the right — together with the prompts status and the Regenerate action; identity and soul previews are editable monospace textareas persisted via the PATCH contract on Save changes, and the bootstrap preview is read-only. Activating Regenerate SHALL ask the user what should change before running: submitting the change-request form enhances the existing documents with the requested change (the old prompts are always included in the model call), and submitting it empty enhances without an instruction. After a successful regeneration the modal SHALL stay open and refetch the agent so the enhanced documents are visible immediately. The tab SHALL note that regeneration keeps the previous version of each file beside it as `<name>.bak`.

#### Scenario: Deploy a new agent
- **WHEN** the user completes all three wizard steps and confirms
- **THEN** the wizard shows its loading state while the create request generates prompts, and the roster includes the new agent with `prompts_status: ready` (generation gates persistence — see agent-prompts)

#### Scenario: Step 3 skippable
- **WHEN** the user deploys without selecting any capability chip or MCP server
- **THEN** the agent is created with an empty `tools` array (no registry tools enabled) and an empty `enabled_mcps` array (no workspace MCP servers enabled)

#### Scenario: Tools come from the catalog
- **WHEN** Step 3 renders the tool chips
- **THEN** chips show the catalog display names with icons (List Files, Read File, Write File, Edit File, Glob, Grep, Web Search, Web Fetch, Browser, Shell) with no group headings, and the set matches the workspace tools endpoint

#### Scenario: Browser is one chip
- **WHEN** the user selects the Browser chip and saves
- **THEN** the stored `tools` array contains the `browser` alias and no individual `browser.*` names

#### Scenario: Legacy browser names normalize
- **WHEN** an agent saved with `browser.navigate` and `browser.read` is opened for edit
- **THEN** the Browser chip shows selected, and saving replaces those names with the alias

#### Scenario: Shell is one chip
- **WHEN** Step 3 renders
- **THEN** shell appears as a single "Shell" chip storing the reserved `execute` name

#### Scenario: Workspace-disabled tool unselectable
- **WHEN** the workspace has disabled `web.search` and the user opens Step 3
- **THEN** the Web Search chip renders greyed with a lock and a tooltip pointing to Settings → Tools, and cannot be toggled on

#### Scenario: MCP toggles default to off
- **WHEN** Step 3 renders for an agent with an empty `enabled_mcps` and the workspace has registered servers
- **THEN** every workspace server row shows an off toggle, and toggling one on stores its id in `enabled_mcps` on save

#### Scenario: Opted-in server hydrates selected
- **WHEN** an agent whose `enabled_mcps` contains a server id is opened for edit
- **THEN** that server's row shows selected, and removing the selection drops the id on save

#### Scenario: Paused server warns on opt-in
- **WHEN** the user toggles on a server currently paused at workspace level
- **THEN** the row renders a paused warning that the server contributes no tools until resumed

#### Scenario: Private server add and remove
- **WHEN** an `agents.write` holder adds a private server through the structured dialog and saves, then reopens the agent
- **THEN** the server is listed under Agent MCP servers; removing it deletes it from that agent only

#### Scenario: Skill options include workspace skills
- **WHEN** the workspace has enabled workspace skills
- **THEN** they render as locked, non-toggleable chips marked always-on alongside system skills, with a hint naming Settings → Skills

#### Scenario: Agent-tier skill add and remove
- **WHEN** an admin adds a skill through the Agent skills section and saves
- **THEN** the skill installs into that agent's skills directory only, listed under Agent skills on the next edit; removing it deletes it from the agent

#### Scenario: Unmet skill dependency warns inline
- **WHEN** a locked workspace skill chip's skill requires `web.search` and the agent's allowlist lacks it
- **THEN** the chip renders a warning naming the missing tool and pointing at the tool chips below

#### Scenario: Provider cascade
- **WHEN** the user switches provider while a model of the first is selected
- **THEN** the model selection resets to the new provider's first model (or empty when the catalog returns none)

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
