# settings delta

## ADDED Requirements

### Requirement: Notifications pane

The settings menu SHALL provide a Notifications pane presenting the notification preferences: a master enable toggle and one toggle per notification category (agent turns, needs attention, schedules, groups). The pane SHALL load the persisted settings on open, save changes durably on toggle, and reflect the saved state. The pane SHALL present identically in both themes.

#### Scenario: Edit preferences

- **WHEN** the user opens the Notifications pane and toggles the Schedules category off
- **THEN** the change is persisted and the pane still shows the master flag enabled with the other categories unchanged

#### Scenario: Master toggle gates categories

- **WHEN** the user turns the master toggle off
- **THEN** the category toggles render as disabled (visually inactive) while keeping their stored values

## MODIFIED Requirements

### Requirement: Settings navigation layout

The settings menu SHALL present a two-column layout: a left settings-nav sidebar and a right content pane. The sidebar SHALL list the settings panes grouped into sections — Notifications under APP first, then Providers under MODELS; Integrations, Channels, Browser Use, and Computer Use under INTEGRATIONS; Skills, Tools, Hooks, and MCP under AUTOMATION — each with an icon and its label, and SHALL offer a search input that filters the pane list by label. The content pane SHALL show the active pane's title, a one-line description, and the pane body. Selecting a sidebar entry switches the content pane.

#### Scenario: Open settings

- **WHEN** the user opens the settings menu
- **THEN** the sidebar shows the grouped pane list, including Integrations, Channels, Browser Use, Computer Use, and Notifications, and the content pane shows the first pane (Providers)

#### Scenario: Switch panes

- **WHEN** the user selects a different pane in the sidebar
- **THEN** the content pane swaps to that pane with its title and description

#### Scenario: Search filters panes

- **WHEN** the user types in the sidebar search input
- **THEN** the pane list filters to entries matching the text, and clearing it restores the full list

#### Scenario: Theme parity

- **WHEN** the user toggles between light and dark themes while in settings
- **THEN** the sidebar, content pane, and provider surfaces render legibly in the selected theme
