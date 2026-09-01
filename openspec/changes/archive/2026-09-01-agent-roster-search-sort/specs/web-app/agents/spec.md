## MODIFIED Requirements

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
Deploying a new agent or editing an existing one SHALL happen through a three-step wizard modal. **Step 1 (Identity)** SHALL expose exactly one labeled control per property — name, slug (auto-suggested from name, editable), role (short, free-form with kebab-case suggestions), description (textarea), goal & behavior brief (textarea, the generation driver), and avatar picker (react-nice-avatar prop controls with randomize). **Step 2 (Model)** SHALL expose provider select (workspace's configured provider configs, API-driven), model combobox (dropdown fed by the model-catalog endpoints, free-text when the catalog returns nothing) with its effort dropdown, and a collapsed "Advanced model configuration" section holding temperature and max_tokens only. **Step 3 (Capabilities, skippable)** SHALL expose tool toggles, skill toggles (workspace skills), MCP server toggles, and the autonomy selector (`approval | suggest | full`) accompanied by a short description of the selected autonomy. One labeled control per property; raw JSON editing MUST NOT be offered for any structured value. Save/create SHALL be disabled until required fields are valid; the collapsed Advanced section SHALL auto-expand on submit if its fields are invalid. Step 3 toggles list only capabilities that exist (existing tools are registry names, skills from `GET /skills`, MCP servers from workspace settings; registries for tools/MCP validation come later).

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
