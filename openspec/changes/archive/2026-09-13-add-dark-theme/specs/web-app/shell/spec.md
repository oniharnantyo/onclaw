## MODIFIED Requirements

### Requirement: Design tokens and typography
The app SHALL render all surfaces using token values resolved per active theme — radii 8/12/16/pill px, motion 150/200ms `cubic-bezier(0.2, 0, 0, 1)`, Inter for body/display text and JetBrains Mono for model names, tools, cron expressions, and metadata, identical in every theme. In the light theme the token values are the frozen design-contract values: background `#fafafa`, surface `#ffffff`, foreground `#111111`, muted `#6b6b6b`, border `#e5e5e5`, accent `#2f6feb`. In the dark theme the token values are the derived dark palette: background `#111111`, surface `#1a1a1a`, elevated-surface tier `#222222`, foreground `#ededed`, muted `#9a9a9a`, border `#2c2c2c`, accent unchanged `#2f6feb`, status hues unchanged. Framework-default theme colors, radii, or typography MUST NOT appear, and component code MUST NOT hardcode theme-specific colors — all color flows through the shared design tokens.

#### Scenario: Token fidelity
- **WHEN** any screen is rendered with the light theme active and inspected
- **THEN** background, surface, text, border, and accent colors resolve to the frozen contract values above (or OKLAB color-mixes of them), and no other font families are used

#### Scenario: Dark theme fidelity
- **WHEN** any screen is rendered with the dark theme active and inspected
- **THEN** background, surface, text, border, and accent colors resolve to the derived dark palette above (or OKLAB color-mixes of them), and the accent and status hues match the light theme's

#### Scenario: Typography and shape are theme-invariant
- **WHEN** the same screen is rendered in light and in dark
- **THEN** font families, type sizes, radii, and motion timings are identical; only color tokens differ

## ADDED Requirements

### Requirement: Theme preference
The app SHALL offer a three-state theme preference — light, dark, system — resolved per device and persisted to browser local storage. The preference SHALL default to system with no stored value, where system resolves to the operating system's `prefers-color-scheme`. A stored light or dark preference SHALL override the OS setting. While the preference is system, the app SHALL follow OS scheme changes live, without reload. The preference SHALL be device-local: it is not workspace data, is not synced across devices, and has no server-side representation.

#### Scenario: Fresh browser follows the OS
- **WHEN** a visitor with no stored theme preference and an OS set to dark opens the app
- **THEN** every screen renders in the dark theme

#### Scenario: Stored preference overrides the OS
- **WHEN** the stored preference is dark and the OS is set to light
- **THEN** every screen renders in the dark theme

#### Scenario: System mode tracks the OS live
- **WHEN** the preference is system and the OS switches its color scheme while the app is open
- **THEN** the app re-renders in the new scheme without a reload

#### Scenario: Preference survives reload
- **WHEN** the user selects dark and reloads the app
- **THEN** the app boots in the dark theme with the cycle control showing dark as the current state

### Requirement: No-flash theme boot
The theme SHALL be resolved and applied to the document before the application bundle executes, so the first paint of any screen — including the login screen and any unauthenticated route — already matches the resolved theme. A reload in a non-light resolved theme MUST NOT paint the light theme first.

#### Scenario: Dark reload has no light flash
- **WHEN** the user reloads any route with dark resolved
- **THEN** the document's first paint uses dark token values

#### Scenario: Pre-auth screens are themed
- **WHEN** a logged-out visitor opens /login with dark resolved
- **THEN** the login screen renders in the dark theme

### Requirement: Theme cycle control
The app SHALL expose one theme control that cycles the preference through light → dark → system → light on each click. The control SHALL appear in exactly two homes: in the nav rail's bottom cluster directly above the Settings row (icon with mode label when the rail is expanded, icon-only with a tooltip when collapsed), and on the login screen as an icon-only button in the viewport's top-right corner. The control's icon SHALL mirror the current mode — sun for light, moon for dark, monitor for system — and when the icon-only form is used, a tooltip SHALL announce the current state and what the next click selects. The control SHALL NOT appear in workspace settings.

#### Scenario: Cycle order
- **WHEN** the user clicks the rail's theme control three times starting from light
- **THEN** the theme moves light → dark → system, and a fourth click returns to light

#### Scenario: Icon mirrors the mode
- **WHEN** the resolved preference is system
- **THEN** the control shows the monitor icon regardless of which scheme the OS currently renders

#### Scenario: Collapsed rail tooltip
- **WHEN** the rail is collapsed and the current state is dark
- **THEN** hovering the control announces the current state and the next selection (system)

#### Scenario: Login screen control
- **WHEN** a logged-out visitor opens /login
- **THEN** an icon-only theme control is present in the viewport's top-right corner and cycles the same three states

#### Scenario: No settings duplicate
- **WHEN** the user opens workspace settings
- **THEN** no theme control or appearance section exists there
