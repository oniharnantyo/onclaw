## MODIFIED Requirements

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/login` (session), `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, `/welcome` for a zero-agent workspace, and `/admin` (instance administration for qualified master-tenant members: `/admin/workspaces` for tenant management, `/admin/accounts` for user management). The active workspace (tenant) SHALL NOT be part of the URL.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens /c/ with an identifier that does not exist in the active workspace
- **THEN** the app redirects to the first agent's chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens a chat route (/ or /c/*)
- **THEN** the app routes to /welcome instead; the Agents, Cron, and Runs screens SHALL render normally in zero-agent workspaces with their empty states

#### Scenario: Unauthenticated access
- **WHEN** a logged-out visitor opens any route other than /login
- **THEN** the app redirects to /login

### Requirement: Position persistence
The app SHALL persist the active workspace, current route, active chat, members-panel open state, and the rail's expanded/collapsed state to browser local storage, restoring them on reload.

#### Scenario: Reload restores position
- **WHEN** the user reloads after switching to workspace "Globex" and opening a channel chat
- **THEN** the app boots directly into that workspace, chat, and members-panel state

#### Scenario: Rail state survives reload
- **WHEN** the user expands the rail and reloads
- **THEN** the rail boots expanded

## ADDED Requirements

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
