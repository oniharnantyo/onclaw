# web-app/shell Specification

## Purpose

The application chrome that every screen renders inside: design tokens, typography, routing between screens, workspace-scoped navigation (rail, sidebar, mobile drawer), and cross-reload position persistence.

## Requirements

### Requirement: Design tokens and typography
The app SHALL render all surfaces using the prototype's frozen token values — background `#fafafa`, surface `#ffffff`, foreground `#111111`, muted `#6b6b6b`, border `#e5e5e5`, accent `#2f6feb`, radii 8/12/16/pill px, motion 150/200ms `cubic-bezier(0.2, 0, 0, 1)` — with Inter for body/display text and JetBrains Mono for model names, tools, cron expressions, and metadata. Framework-default theme colors, radii, or typography MUST NOT appear.

#### Scenario: Token fidelity
- **WHEN** any screen is rendered and inspected
- **THEN** background, surface, text, border, and accent colors resolve to the token values above (or OKLAB color-mixes of them), and no other font families are used

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/login` (session), `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, `/welcome` for a zero-agent workspace, `/settings` and `/settings/:section` (workspace settings: workspace, providers, members, integrations, mcp, skills, keys, notifications), and `/admin` (instance administration for qualified master-tenant members: `/admin/workspaces` for tenant management, `/admin/accounts` for user management). The active workspace (tenant) SHALL NOT be part of the URL. A URL matching no route SHALL render a 404 error state in place (see the error-states capability), not a redirect to an arbitrary screen.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens /c/ with an identifier that does not exist in the active workspace
- **THEN** the app renders a not-found error state in place instead of redirecting to another chat

#### Scenario: Unknown URL
- **WHEN** a visitor opens a URL matching no route
- **THEN** the app renders a 404 error state in place instead of redirecting to a random chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens a chat route (/ or /c/*)
- **THEN** the app routes to /welcome instead; the Agents, Cron, and Runs screens SHALL render normally in zero-agent workspaces with their empty states

#### Scenario: Unauthenticated access
- **WHEN** a logged-out visitor opens any route other than /login
- **THEN** the app redirects to /login

#### Scenario: Settings deep link
- **WHEN** a member opens /settings/keys directly
- **THEN** the keys section of workspace settings renders at that URL; /settings redirects to /settings/workspace

### Requirement: Responsive navigation
The icon rail SHALL remain visible at every viewport width. The sidebar SHALL render as a static column at viewport widths ≥768px and as an off-canvas drawer below 768px, opened from a control in the rail and dismissible. The channel members panel SHALL render as a static column at widths ≥1280px and as a slide-over sheet below 1280px. At every width in the contract viewport matrix (360×800 through 1920×1080) the app MUST NOT scroll horizontally.

#### Scenario: Mobile navigation
- **WHEN** the viewport is 390px wide
- **THEN** the rail is visible, the sidebar is hidden until opened as a drawer, and no horizontal scrollbar appears

#### Scenario: Tablet width
- **WHEN** the viewport is 820px wide
- **THEN** the sidebar renders as a static column beside the chat

### Requirement: Position persistence
The app SHALL persist the active workspace, current route, active chat, members-panel open state, and the rail's expanded/collapsed state to browser local storage, restoring them on reload.

#### Scenario: Reload restores position
- **WHEN** the user reloads after switching to workspace "Globex" and opening a channel chat
- **THEN** the app boots directly into that workspace, chat, and members-panel state

#### Scenario: Rail state survives reload
- **WHEN** the user expands the rail and reloads
- **THEN** the rail boots expanded

### Requirement: Keyboard search focus
The app SHALL focus the sidebar search input when the user presses Cmd/Ctrl+K.

#### Scenario: Command-K
- **WHEN** the user presses Cmd+K (or Ctrl+K)
- **THEN** the sidebar search input receives focus

### Requirement: Rail tooltips and expandable labels
The rail SHALL show a styled tooltip containing the item's label when an icon-only rail control receives pointer hover or keyboard focus. A rail control SHALL toggle the rail between collapsed (icons only, the default) and expanded (icons with text labels); the expanded state SHALL reveal every rail item's label, and the toggle SHALL be available at viewport widths ≥768px and hidden below (the drawer provides text navigation at those widths).

#### Scenario: Hover tooltip
- **WHEN** a user hovers a collapsed rail item
- **THEN** its label appears in a styled tooltip without any native-title duplication

#### Scenario: Focus tooltip
- **WHEN** a keyboard user tabs onto a rail item
- **THEN** the same tooltip appears without pointer hover

#### Scenario: Expand reveals labels
- **WHEN** the user activates the expand toggle
- **THEN** text labels render beside every rail icon and badges relocate inline; collapsing restores icons-only

#### Scenario: Toggle hidden on mobile
- **WHEN** the viewport is below 768px
- **THEN** the toggle is not rendered; the drawer provides text navigation

### Requirement: Agent rows in navigation
Sidebar agent rows SHALL render the agent's avatar configuration when set (initials tile when empty), matching the roster cards, and SHALL list agents newest-created first (the store order). The sidebar ⌘K search SHALL match agent name and role — a deliberately broader scope than the roster's name-only search, so navigation surfaces agents by what they do while the roster narrows by identity.

#### Scenario: Avatar in sidebar rows
- **when** an agent has an avatar configuration
- **then** its sidebar row renders that avatar instead of the initials tile; an agent with an empty avatar keeps the initials fallback

#### Scenario: Newest-first sidebar order
- **when** agents "A", "B", "C" are created in that order
- **then** the sidebar lists them C, B, A

#### Scenario: Sidebar search scope unchanged
- **when** the user types a role word into the ⌘K search
- **then** matching agent rows appear in the sidebar; the roster's name-only search is unaffected and vice versa

### Requirement: User memory editor in the user menu
The user info menu SHALL include an entry opening the signed-in member's own `USER.md` memory editor for the active workspace: a modal with a free-form textarea (memory is unstructured markdown), a live size/token counter fed by the server cap, and save via the user memory endpoint. Saving past the cap SHALL surface the 422 field error inline; successful save SHALL confirm. Other members' memories are never reachable from this surface.

#### Scenario: Member opens and edits own memory
- **WHEN** a member opens the user menu, chooses the memory entry, edits the textarea, and saves
- **THEN** the content persists via the user memory endpoint and the editor reflects the saved state

#### Scenario: Over-cap save shows field error
- **WHEN** the member saves content past the size cap
- **THEN** the 422 error is shown inline and the modal stays open with the content intact
