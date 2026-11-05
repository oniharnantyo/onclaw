# settings Specification (delta)

## ADDED Requirements

### Requirement: Browser Use pane

The Browser Use pane SHALL configure the Browser Use system server: a server status line fed by its live connection state, engine settings (default profile chosen from existing profiles, headless toggle defaulting on, idle timeout seconds defaulting 120, action timeout seconds defaulting 30, allow-private toggle defaulting off), a profile list with create and delete controls, and a profile import action for existing Chrome/Chromium profiles with the import disclosures. Engine configuration SHALL persist under the browser tool settings key and apply to the server at its next spawn boundary. Creating a profile SHALL create its directory; deleting a profile SHALL remove it after its browser closes; the `default` profile SHALL not be deletable; deleting the configured default profile SHALL reset the default to `default`.

#### Scenario: Configuration round-trips

- **WHEN** the user sets headless off, idle timeout 300, and default profile `work` in the pane and saves
- **THEN** reopening the pane shows those values and a subsequent server spawn honors them

#### Scenario: Profile create and delete

- **WHEN** the user creates profile `work` and later deletes it
- **THEN** `~/.buddies/browser/profiles/work/` exists after creation, and after deletion it is gone and no longer listed

#### Scenario: Import disclosures are shown

- **WHEN** the user opens the profile import action
- **THEN** the flow explains closing the source browser, the copy size expectation, and best-effort cookie decryption before starting the import

#### Scenario: Server status line reflects the system server

- **WHEN** the Browser Use server process is in a failed supervision state
- **THEN** the pane's status line shows the failed state with the reason

## MODIFIED Requirements

### Requirement: Settings navigation layout

The settings menu SHALL present a two-column layout: a left settings-nav sidebar and a right content pane. The sidebar SHALL list the settings panes grouped into sections — Providers under MODELS; Integrations, Channels, and Browser Use under INTEGRATIONS; Skills, Tools, Hooks, and MCP under AUTOMATION — each with an icon and its label, and SHALL offer a search input that filters the pane list by label. The content pane SHALL show the active pane's title, a one-line description, and the pane body. Selecting a sidebar entry switches the content pane.

#### Scenario: Open settings

- **WHEN** the user opens the settings menu
- **THEN** the sidebar shows the grouped pane list, including Integrations, Channels, and Browser Use, and the content pane shows the first pane (Providers)

#### Scenario: Switch panes

- **WHEN** the user selects a different pane in the sidebar
- **THEN** the content pane swaps to that pane with its title and description

#### Scenario: Search filters panes

- **WHEN** the user types in the sidebar search input
- **THEN** the pane list filters to entries matching the text, and clearing it restores the full list

#### Scenario: Theme parity

- **WHEN** the user toggles between light and dark themes while in settings
- **THEN** the sidebar, content pane, and provider surfaces render legibly in the selected theme
