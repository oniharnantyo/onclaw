# Spec Delta

## ADDED Requirements

### Requirement: Application menu with settings entry

The desktop host (macOS) SHALL install a custom application menu whose app submenu contains, in order: "About buddies", "Settings…" (accelerator ⌘,), Services, Hide, Hide Others, Show All, and Quit — with "Settings…" placed directly below the About entry. The rest of the menu bar SHALL retain the standard Edit and Window menus so clipboard and window shortcuts keep working. Activating "Settings…" SHALL make the main window visible if hidden and SHALL navigate the frontend to the settings menu. The server host MUST NOT depend on this menu (headless, no menu bar).

#### Scenario: Menu structure

- **WHEN** the user opens the application menu (the app's name menu in the macOS menu bar)
- **THEN** "Settings…" appears directly below "About buddies" and shows the ⌘, accelerator

#### Scenario: Open settings from the menu while hidden

- **WHEN** the user activates "Settings…" while the main window is hidden (closed to the tray)
- **THEN** the main window becomes visible and the settings menu is displayed

#### Scenario: Open settings from the menu while visible

- **WHEN** the user activates "Settings…" while the main window is visible on another menu
- **THEN** the frontend switches to the settings menu without creating a second window

#### Scenario: Settings keyboard shortcut

- **WHEN** the user presses ⌘, anywhere in the app
- **THEN** the main window is shown (if hidden) and the settings menu is displayed
