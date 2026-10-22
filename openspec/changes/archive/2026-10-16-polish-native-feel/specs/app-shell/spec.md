# Spec Delta

## ADDED Requirements

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
