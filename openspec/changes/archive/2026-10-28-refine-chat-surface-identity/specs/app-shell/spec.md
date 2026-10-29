# Spec Delta

## MODIFIED Requirements

### Requirement: Window frame persistence

The desktop host SHALL remember the main window's size and position across
app restarts: on relaunch the window SHALL open with the frame it had when
it was last closed or when the app last quit. The frame SHALL be restored
per the saved values even across display changes, clamped to remain visible
on currently connected displays. Multi-monitor exact placement is a
best-effort concern; the saved frame MUST NOT strand the window off-screen.
The window SHALL also enforce a minimum size, measured to keep the chat
view's composer control rail usable: the user MUST NOT be able to resize
below the minimum, and the persisted frame MUST NOT record a size below it.

#### Scenario: Frame survives restart

- **WHEN** the user moves/resizes the window, quits the app, and relaunches
- **THEN** the window opens with the same size and position

#### Scenario: Saved frame is off-screen

- **WHEN** the saved frame lies outside all currently connected displays
  (e.g. a monitor was unplugged)
- **THEN** the window opens at a visible position instead of the raw saved
  frame

#### Scenario: Resize stops at the minimum

- **WHEN** the user drags the window edge below the minimum size
- **THEN** the window stops at the minimum size, and a subsequent restart
  restores that size rather than anything smaller
