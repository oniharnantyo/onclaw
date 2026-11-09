## ADDED Requirements

### Requirement: Computer Use pane

The settings menu SHALL present a Computer Use pane with: an enable toggle; a system-permissions section listing each required permission with its live granted state, a Manage action opening the matching system settings pane, and a Check again action that re-probes and applies a grant change (restarting the driver when one occurred); a one-time download state for the vendored driver with its verification outcome; and a status line fed by the live supervision state (ready, awaiting permissions, downloading, failed with reason, or unavailable in server mode). The pane SHALL work in both app themes.

#### Scenario: Fresh enable shows the permission path

- **WHEN** the user enables Computer Use before granting permissions
- **THEN** the pane lists the missing permissions with Manage actions, the status line reports awaiting permissions, and no daemon is running

#### Scenario: Check again applies a grant

- **WHEN** the user grants a missing permission in system settings and presses Check again
- **THEN** the pane re-probes, reports the permission granted, and the status line advances toward ready
