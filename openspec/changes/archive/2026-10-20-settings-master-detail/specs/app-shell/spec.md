# Spec Delta

## REMOVED Requirements

### Requirement: Settings opens as a full-island view

Opening Settings (rail item, ⌘,, or the application menu) SHALL replace the
entire work area with the settings view as a single island. The chat session
layer and any chat-level toolbar SHALL NOT render while Settings is open, and
leaving Settings SHALL restore the chat layout exactly as it was.

#### Scenario: Settings replaces chat layout

- **WHEN** the user opens Settings from the rail
- **THEN** the work area shows only the settings island (pane sidebar + pane
  content) and no session column or chat toolbar is visible

#### Scenario: Returning to chat restores layout

- **WHEN** the user selects Chat after using Settings
- **THEN** the Sessions island and chat surface reappear with the previous
  session still selected

**Reason**: Settings taking over the whole window hides the navigation rail and
makes settings feel like a separate mode; the reference UX keeps the left
navigation visible in every menu.

**Migration**: replaced by "Settings renders inside the content island", which
keeps the chat-exclusion and layout-restore guarantees while the rail stays
visible.

## ADDED Requirements

### Requirement: Settings renders inside the content island

Opening Settings (rail item, ⌘,, or the application menu) SHALL show the
settings view inside the content island with the navigation rail remaining
visible and its Settings row active. The chat session layer and any
chat-level toolbar SHALL NOT render while Settings is open. Leaving Settings
SHALL restore the chat layout exactly as it was. No interactive control SHALL
sit in the window's invisible-titlebar drag band; the settings island's
columns SHALL clear that band like the other island views. The window strip
SHALL identify Settings with its title text, as it does for the other island
views. The settings view SHALL NOT render a separate back affordance; the
rail is the navigation.

#### Scenario: Settings opens with the rail visible

- **WHEN** the user opens Settings from the rail, ⌘,, or the application menu
- **THEN** the content island shows the settings view (pane nav + pane
  content), the rail stays visible with its Settings row active, and no
  session column or chat toolbar is visible

#### Scenario: Returning to chat restores layout

- **WHEN** the user selects Chat after using Settings
- **THEN** the Sessions island and chat surface reappear with the previous
  session still selected

#### Scenario: Settings respects the drag band and the strip

- **WHEN** the user views or drags the window strip while Settings is open
- **THEN** the strip shows the Settings title as non-interactive text, no
  interactive control sits in the drag band, and dragging the strip moves the
  window
