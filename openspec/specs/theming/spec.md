# theming Specification

## Purpose

The theme mechanism as observable behavior: two themes (light and dark) that switch at
runtime, follow the system preference until the user chooses, keep native chrome consistent with
the page, and degrade to opaque rendering under reduced transparency — the contract every UI
surface and future styling work must hold.

## Requirements

### Requirement: Runtime theme switching

The app SHALL offer exactly two themes, light and dark, and switching between them SHALL restyle
every visible surface — shell, rail, panes, dialogs, forms, and toasts — without a reload, a
flash of unstyled content, or a loss of legibility in either direction.

#### Scenario: Toggle restyles the whole window

- **WHEN** the user toggles the theme from any screen
- **THEN** all visible surfaces (including already-open dialogs and menus) switch to the selected
  theme immediately, without a page reload

#### Scenario: Both themes fully legible

- **WHEN** each screen of the app is viewed in the light theme and in the dark theme
- **THEN** text, icons, borders, and controls meet readable contrast against their surfaces in
  both themes

### Requirement: System-preference default before an explicit choice

Until the user makes an explicit theme choice in a session, the app SHALL render the theme
matching the system color-scheme preference from first paint — light on light-preference systems,
dark on dark-preference systems — with no flash of the wrong theme.

#### Scenario: First paint follows system appearance

- **WHEN** the app starts on a system set to dark appearance and the user has not chosen a theme
- **THEN** the first rendered frame is already dark; the light theme never flashes

#### Scenario: Explicit choice wins for the session

- **WHEN** the user explicitly selects the opposite theme from the system preference
- **THEN** the selected theme renders and survives in-app navigation without reverting to the
  system preference

#### Scenario: System choice tracks OS changes live

- **WHEN** the theme preference is set to "system" and the OS switches appearance while the app
  is running
- **THEN** the app re-resolves and applies the new theme without a reload or manual toggle

### Requirement: Native chrome follows the active theme

The native `color-scheme` of the window SHALL match the active theme so browser-provided surfaces
— form controls, scrollbars, and text-selection highlight — render in the matching scheme, on
both desktop and server-mode web.

#### Scenario: Native controls match the theme

- **WHEN** the user views native select dropdowns, scrollbars, or selected text while the dark
  theme is active
- **THEN** those native surfaces render in the dark scheme, not light

### Requirement: Reduced transparency renders opaque

Under the system `prefers-reduced-transparency: reduce` setting, the app SHALL render with fully
opaque theme surfaces — the transparency/glass layer SHALL not leave text over blurred or
translucent backing — while remaining legible in both themes.

#### Scenario: Reduced transparency disables glass

- **WHEN** the system sets reduced transparency and the user views the shell and chat surfaces
- **THEN** backgrounds render opaque and all text remains legible in both themes
