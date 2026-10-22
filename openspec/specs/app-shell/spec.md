# app-shell Specification

## Purpose

Provide the single application shell that runs buddies in two hosts — a desktop host (window + system tray) and a headless server host (the same frontend served as a web app) — so the gateway and all services keep running regardless of window visibility or host.

## Requirements

### Requirement: Desktop background lifecycle

The desktop host SHALL keep the application running when the main window is closed; closing the window MUST hide it, not terminate the process. Background services (gateway, schedulers) MUST continue running while the window is hidden.

#### Scenario: User closes the window

- **WHEN** the user closes the main window on the desktop host
- **THEN** the window disappears, the process keeps running, and background services continue operating

#### Scenario: Reopening after close

- **WHEN** the user activates the tray menu item "Open" while the window is hidden
- **THEN** the main window becomes visible again with its previous state

### Requirement: System tray presence

The desktop host SHALL display a system tray icon whenever the app is running. The tray menu SHALL offer, at minimum: open the main window, show gateway running state, pause/resume the gateway, and quit the application. Quitting from the tray MUST be the only way to fully exit the app after the window has been closed.

#### Scenario: Tray menu contents

- **WHEN** the user opens the system tray menu
- **THEN** the menu shows an item to open the window, the current gateway state with a pause/resume toggle, and a quit item

#### Scenario: Quit from tray terminates the app

- **WHEN** the user selects Quit in the tray menu
- **THEN** the process terminates cleanly, stopping background services first

### Requirement: Server host serves the same app as web

The application SHALL build in a headless server mode (separate binary) that serves the same frontend and exposes the same bound services over HTTP/WebSocket as the desktop host. Server mode MUST NOT require native GUI libraries. The server SHALL bind to a loopback address by default and SHALL NOT accept remote connections unless explicitly configured.

#### Scenario: Web access to the same app

- **WHEN** the server-mode binary is running and a browser opens the configured address
- **THEN** the same frontend loads and the user can chat with agents with the same data as the desktop host

#### Scenario: Default binding is loopback

- **WHEN** the server-mode binary starts without an explicit bind address
- **THEN** it listens only on a loopback interface

### Requirement: Optional server bearer token

Server mode SHALL support an optional bearer token; when configured, requests and WebSocket connections without the token MUST be rejected. When not configured, no authentication is required (local single-user assumption).

#### Scenario: Token required when configured

- **WHEN** a token is configured and a client connects without presenting it
- **THEN** the request or connection is rejected

#### Scenario: Token absent from config

- **WHEN** no token is configured
- **THEN** clients connect without authentication

### Requirement: Service parity across hosts

All application services (agents, sessions, configuration, settings) SHALL be callable from both hosts through the same service API surface. Behavior MUST NOT differ between desktop and server hosts except for host-specific concerns (window, tray).

#### Scenario: Same operations in both hosts

- **WHEN** the user creates an agent and sends a chat message on the desktop host, then opens the server host's web UI against the same data directory
- **THEN** the agent exists and the session history is identical

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

### Requirement: Single-instance launch

The desktop host SHALL enforce single-instance launch: when a second copy of
the app starts, it SHALL NOT create a second process-visible instance
(second tray icon, second gateway, second store handle); instead the
existing instance's main window SHALL become visible (shown, unminimized)
and the second launch SHALL exit. The server host MUST NOT depend on this
behavior (multiple server processes are a deployment concern, not the
desktop launch path).

#### Scenario: Second launch focuses the running app

- **WHEN** the app is already running (window visible or hidden to the tray)
  and the user launches it again
- **THEN** no second tray icon appears, and the existing instance's main
  window becomes visible

#### Scenario: Second launch while window is hidden

- **WHEN** the running instance's window is hidden (closed to the tray) and
  a second launch occurs
- **THEN** the existing instance shows its main window and the second
  process exits without touching the store

### Requirement: Truthful bundle identity

The packaged desktop app SHALL present the buddies identity, not placeholder
values: the dock icon and the `.icns` in the app bundle SHALL be built from
the buddies app icon; the tray SHALL use a monochrome template glyph
appropriate for the macOS menu bar (platform-appropriate variant on other
OSes); and the bundle metadata (bundle name, copyright/get-info strings,
version) SHALL carry the buddies values — the same values the macOS About
panel displays. Development builds MAY keep placeholder strings, but the
About panel SHALL NOT show template text such as "My Product".

#### Scenario: About panel shows buddies identity

- **WHEN** the user opens the About panel from the app menu
- **THEN** the app name and version shown are the buddies values, with no
  template placeholder strings

#### Scenario: Dock and tray icons

- **WHEN** the app runs from the packaged bundle
- **THEN** the dock shows the buddies app icon and the tray shows the
  buddies template glyph (monochrome on macOS), not the framework
  placeholder

### Requirement: No default web context menu

The desktop window SHALL NOT present the system WebView's default context
menu (Reload, Inspect, web text actions) on right-click or long-press. The
app MAY introduce its own context affordances later; the requirement is the
absence of the browser menu. The server-mode web frontend is not constrained
by this requirement (a browser supplies its own menu there).

#### Scenario: Right-click in the desktop window

- **WHEN** the user right-clicks anywhere in the desktop app window
- **THEN** no web context menu (Reload / Inspect / Look Up) appears

### Requirement: Window frame persistence

The desktop host SHALL remember the main window's size and position across
app restarts: on relaunch the window SHALL open with the frame it had when
it was last closed or when the app last quit. The frame SHALL be restored
per the saved values even across display changes, clamped to remain visible
on currently connected displays. Multi-monitor exact placement is a
best-effort concern; the saved frame MUST NOT strand the window off-screen.

#### Scenario: Frame survives restart

- **WHEN** the user moves/resizes the window, quits the app, and relaunches
- **THEN** the window opens with the same size and position

#### Scenario: Saved frame is off-screen

- **WHEN** the saved frame lies outside all currently connected displays
  (e.g. a monitor was unplugged)
- **THEN** the window opens at a visible position instead of the raw saved
  frame

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
