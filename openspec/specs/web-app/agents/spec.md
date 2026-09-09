# web-app/agents Specification

## Purpose

The agents screen — the roster of workspace agents as cards — and the structured configuration modal used to deploy a new agent or edit an existing one.

## Requirements

### Requirement: Agents roster
The agents screen SHALL list every agent in the active workspace as a card showing avatar, name, an identity line with role and model in monospace (role omitted when unset or identical to the name — no dangling separator), an autonomy chip, a one-line description (omitted when unset or identical to the name — the card renders no placeholder text), and actions to open the chat or configure the agent. The card SHALL NOT render tool chips, a live status dot, or a last-active timestamp. The model id SHALL appear exactly once on the card (the identity line). A toolbar between the header and grid SHALL provide a search input matching agent names only (case-insensitive substring) and a sort select offering Newest first (default), Name A–Z, and Oldest. While a search is active the header sub line SHALL read "N of M agents …". When more than 24 agents match, a pager footer SHALL offer Prev/Next with a "Page X of N" label (24 cards per page, a size that divides evenly into the 3/2/1-column grid); the pager SHALL be hidden when 24 or fewer agents match, SHALL reset to page 1 when the search or sort changes, and SHALL clamp to the last page when the result set shrinks below the current page. Agents in error state SHALL show a "Needs attention" notice. The grid SHALL flow 1 column below 640px, 2 at ≥640px, and 3 at ≥1280px.

#### Scenario: Error-state agent
- **WHEN** agent "Warden" has status `error`
- **THEN** its card shows a danger-colored "Needs attention — see latest run" notice

#### Scenario: Empty roster
- **WHEN** the workspace has no agents
- **THEN** the grid shows an empty state inviting the user to deploy one

#### Scenario: Search matches names only
- **WHEN** the user types part of an agent's name into the roster search
- **THEN** only agents whose name contains the query render; an agent whose role or description matches but whose name does not is not shown

#### Scenario: No-match empty state
- **WHEN** no agent name contains the query
- **THEN** the grid shows a "No agents match" empty state naming the query, with a Clear search action that empties the input and restores the full roster

#### Scenario: Filtered count
- **WHEN** a search query is active
- **THEN** the header sub line reads "N of M agents in <workspace> …" where N is the match count and M the workspace total

#### Scenario: Sort options and default
- **WHEN** the roster loads
- **THEN** cards are ordered newest-created first (the store order); selecting "Name A–Z" orders case-insensitively by name; selecting "Oldest" reverses to creation order

#### Scenario: Pager navigation
- **WHEN** more than 24 agents match the active filter
- **THEN** a pager footer renders with Prev/Next and "Page X of N"; Prev is disabled on page 1, Next on the last page, and activating Next reveals cards 25–48

#### Scenario: Pager hidden on a single page
- **WHEN** 24 or fewer agents match
- **THEN** no pager footer renders

#### Scenario: Pager resets on filter change
- **WHEN** the user is on page 2 and changes the search query or sort
- **THEN** the pager returns to page 1

#### Scenario: Pager clamps when results shrink
- **WHEN** the user is on the last page and a new search shrinks matches below the current page
- **THEN** the grid shows the final page of matches instead of an empty page

#### Scenario: Avatar on cards
- **WHEN** an agent has an avatar configuration
- **THEN** its card renders the avatar; agents with an empty avatar render the initials fallback

#### Scenario: Model appears once
- **WHEN** an agent card renders
- **THEN** the model id appears exactly once, in the identity line, with no duplicate chip

#### Scenario: Role appears in the identity line
- **WHEN** an agent has a role distinct from its name
- **THEN** the identity line reads "<role> · <model>" in monospace; when the role is unset or echoes the name, the line shows the model alone with no dangling separator

#### Scenario: No operational telemetry on the card
- **WHEN** an agent card renders, regardless of granted tools or recent activity
- **THEN** no tool chips, no live status dot, and no last-active timestamp appear anywhere on the card

#### Scenario: Description line never echoes the name
- **WHEN** an agent's description is unset or identical to its name
- **THEN** the card renders no description line and no placeholder text in its place

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
### Requirement: Agent status semantics
Agent status SHALL display as Running (pulsing dot), Idle (muted dot), or error state ("Needs attention"), consistently in the sidebar, chat header, and agent cards.

#### Scenario: Running agent indicator
- **WHEN** an agent is running
- **THEN** its status dot pulses and the chat header reads "Running · last active …"

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

### Requirement: Agent slug is create-only
The wizard SHALL expose the slug as an editable, auto-suggested control in create mode only. In edit mode the slug SHALL be displayed read-only (as identity text, not an input), the update payload SHALL omit the slug, and any slug a client still sends SHALL be ignored by the server (see the agents capability).

#### Scenario: Edit mode shows slug read-only
- **WHEN** the agent wizard opens in edit mode
- **THEN** the slug appears as read-only identity text and no slug input is rendered

#### Scenario: Update payload omits slug
- **WHEN** the wizard saves an edit
- **THEN** the PATCH payload carries no slug field

### Requirement: Agent hooks configuration
The agent configuration modal SHALL offer a Hooks section with two parts: CRUD for hooks private to this agent (same editor contract as the workspace Hooks pane, minus the level controls), and a read-only list of the instance- and workspace-level hooks that reach this agent — each shown with its event, what it selects, whether it can block, and its level. The read-only list MUST NOT offer disable or exclusion controls for instance or workspace hooks.

#### Scenario: Agent-owner adds a private gate
- **WHEN** a user who can edit the agent creates an agent-level hook selecting only the shell tool
- **THEN** the hook applies to this agent's future runs immediately and appears under this agent's hooks, not the workspace list

#### Scenario: Visibility without control
- **WHEN** a user views an agent's Hooks section where a mandatory instance hook and a workspace hook apply
- **THEN** both are listed read-only with their level marked, and neither offers a disable control
