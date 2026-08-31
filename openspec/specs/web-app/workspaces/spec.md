# web-app/workspaces Specification

## Purpose

Workspace (tenant) lifecycle in the UI: switching between workspaces, creating one with an optional starter agent, deleting one, and the zero-agent onboarding screen.

## Requirements

### Requirement: Workspace switching
A switcher SHALL list the user's real memberships from the session (`GET /auth/me` payload) with role badges, suspended tenants marked and unselectable. Switching SHALL replace the active workspace with that membership's workspace, navigate to its first agent's chat (or `/welcome` when zero agents), clear channel unread counts for the newly opened target, and show a confirmation toast. The switcher lists memberships only — it offers no creation entry.

#### Scenario: Switch workspace
- **WHEN** the user picks "Globex" from the switcher while in an Acme chat
- **THEN** the app navigates to Globex's first agent chat and toasts "Switched to Globex"

#### Scenario: Suspended membership
- **WHEN** a membership's workspace is suspended
- **THEN** the switcher marks it and switching into it is blocked

### Requirement: Onboarding screen
A workspace with zero agents SHALL present the onboarding screen: workspace-ready headline, explanation that a workspace is a real container now (backed by the API) but empty until it has agents, a primary "Deploy your first agent" action opening the agent configuration modal, a secondary path to workspace settings, and a timezone footer without plan text.

#### Scenario: Deploy first agent from onboarding
- **WHEN** the user clicks "Deploy your first agent" and completes the modal
- **THEN** the new agent's chat opens and onboarding no longer appears for this workspace

#### Scenario: Timezone footer without plan
- **WHEN** the onboarding footer renders
- **THEN** the footer shows the timezone only — no plan text

### Requirement: Workspace leave
A member (non-owner) SHALL be able to leave a workspace from the settings danger zone: one confirm click within a 4-second window, then the workspace disappears from the switcher and the app switches to another membership. Leaving is refused for the last owner — the server's `last_owner_protected` rejection surfaces as a toast.

#### Scenario: Leave and land elsewhere
- **WHEN** a non-owner member leaves the active workspace while belonging to two
- **THEN** the app switches to the remaining membership and toasts the switch

#### Scenario: Last-owner leave refused
- **WHEN** the only owner attempts to leave
- **THEN** the server rejects with last_owner_protected; a toast explains, nothing changes
