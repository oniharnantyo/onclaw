# Spec Delta

## Purpose

Converge the desktop shell on the preview-validated layout: a title-only
window strip, a collapsible navigation rail, and Settings opening as a full
island of the work area. The strip carries only text so the stock macOS
invisible-titlebar drag band stays exactly as shipped.

## ADDED Requirements

### Requirement: Title-only window strip

The desktop window strip SHALL show only the active view's name and one
context line as non-interactive text ("Chat" / session and running counts;
"Settings" / active-pane summary), with no buttons and no island container.
No interactive control SHALL be placed inside the window's invisible-titlebar
drag region (top 50px). Web (non-desktop) rendering of the strip MAY include
the wordmark, matching the existing shell split.

#### Scenario: Strip reflects the active view

- **WHEN** the user switches between Chat and Settings
- **THEN** the strip's title and context line update to describe the active
  view and no interactive controls appear in the strip

#### Scenario: Window still drags by the strip

- **WHEN** the user drags from the empty strip area
- **THEN** the window moves, because the strip contains only text and the
  native drag band is unchanged

### Requirement: Collapsible navigation rail

The navigation rail SHALL offer a toggle in its header that collapses it to
an icon-only width (labels hidden) and back. The collapsed state SHALL keep
all rail actions reachable by icon, SHALL persist across reloads of the web
frontend, and both states SHALL render legibly in both themes.

#### Scenario: Collapse to icons

- **WHEN** the user activates the rail's collapse toggle
- **THEN** the rail shrinks to icon-only width with labels hidden and the
  work area widens

#### Scenario: Collapse state survives reload

- **WHEN** the user reloads the app after collapsing the rail
- **THEN** the rail reopens in the collapsed state

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
