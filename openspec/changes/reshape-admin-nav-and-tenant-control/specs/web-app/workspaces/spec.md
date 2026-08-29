## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Workspace creation
**Reason**: Workspace creation consolidates into the superadmin control plane — the Tenants screen is the single creation path, so self-service creation (switcher entry + modal) is removed from the web app.
**Migration**: Tenants are created from Admin → Workspaces (owner picked from existing users); the workspace switcher lists memberships only. The backend self-service endpoint remains but is unused by the web app.

