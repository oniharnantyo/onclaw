# Capability: web-app/shell

## Purpose

The application chrome that every screen renders inside: design tokens, typography, routing between screens, workspace-scoped navigation (rail, sidebar, mobile drawer), and cross-reload position persistence.

## ADDED Requirements

### Requirement: Design tokens and typography
The app SHALL render all surfaces using the prototype's frozen token values — background `#fafafa`, surface `#ffffff`, foreground `#111111`, muted `#6b6b6b`, border `#e5e5e5`, accent `#2f6feb`, radii 8/12/16/pill px, motion 150/200ms `cubic-bezier(0.2, 0, 0, 1)` — with Inter for body/display text and JetBrains Mono for model names, tools, cron expressions, and metadata. Framework-default theme colors, radii, or typography MUST NOT appear.

#### Scenario: Token fidelity
- **WHEN** any screen is rendered and inspected
- **THEN** background, surface, text, border, and accent colors resolve to the token values above (or OKLAB color-mixes of them), and no other font families are used

### Requirement: Screen routing
Each user-facing screen SHALL be its own route: `/c/:chatId` (agent chat, channel chat, or teammate direct message), `/agents`, `/cron`, `/runs`, and `/welcome` for a zero-agent workspace. The active workspace (tenant) SHALL NOT be part of the URL.

#### Scenario: Unknown chat identifier
- **WHEN** a visitor opens `/c/` with an identifier that does not exist in the active workspace
- **THEN** the app redirects to the first agent's chat

#### Scenario: Zero-agent workspace
- **WHEN** the active workspace has no agents and the visitor opens any chat route
- **THEN** the app routes to `/welcome` instead

### Requirement: Responsive navigation
The icon rail SHALL remain visible at every viewport width. The sidebar SHALL render as a static column at viewport widths ≥768px and as an off-canvas drawer below 768px, opened from a control in the rail and dismissible. The channel members panel SHALL render as a static column at widths ≥1280px and as a slide-over sheet below 1280px. At every width in the contract viewport matrix (360×800 through 1920×1080) the app MUST NOT scroll horizontally.

#### Scenario: Mobile navigation
- **WHEN** the viewport is 390px wide
- **THEN** the rail is visible, the sidebar is hidden until opened as a drawer, and no horizontal scrollbar appears

#### Scenario: Tablet width
- **WHEN** the viewport is 820px wide
- **THEN** the sidebar renders as a static column beside the chat

### Requirement: Position persistence
The app SHALL persist the active workspace, current route, active chat, and members-panel open state to browser local storage, restoring them on reload.

#### Scenario: Reload restores position
- **WHEN** the user reloads after switching to workspace "Globex" and opening a channel chat
- **THEN** the app boots directly into that workspace, chat, and members-panel state

### Requirement: Keyboard search focus
The app SHALL focus the sidebar search input when the user presses Cmd/Ctrl+K.

#### Scenario: Command-K
- **WHEN** the user presses Cmd+K (or Ctrl+K)
- **THEN** the sidebar search input receives focus
