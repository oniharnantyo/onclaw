## MODIFIED Requirements

### Requirement: Workspace switching
A switcher SHALL list the user's real memberships from the session (`GET /auth/me` payload) with role badges, suspended tenants marked and unselectable. Switching SHALL replace the active workspace with that membership's workspace, navigate to its first agent's chat (or `/welcome` when zero agents), clear channel unread counts for the newly opened target, and show a confirmation toast. The switcher lists memberships; its only creation affordance SHALL render exclusively to instance administrators (master-workspace members holding admin.workspaces.write) and open the workspace-creation flow — no other role sees a creation entry.

#### Scenario: Switch workspace
- **WHEN** the user picks "Globex" from the switcher while in an Acme chat
- **THEN** the app navigates to Globex's first agent chat and toasts "Switched to Globex"

#### Scenario: Suspended membership
- **WHEN** a membership's workspace is suspended
- **THEN** the switcher marks it and switching into it is blocked

#### Scenario: Creation entry is admin-only
- **WHEN** a user who is not an instance administrator opens the workspace switcher
- **THEN** no creation affordance renders; an instance administrator sees the creation entry

### Requirement: Workspace creation flow
The workspace-creation flow SHALL be reachable only by instance administrators — via the switcher's creation affordance and the admin console — and SHALL add a provider step and an optional starter-agent step before birth: the provider step collects type, name, base_url (when the type requires it), and the API key, previews models via the credential-preview endpoint for the model choice, and the starter-agent step collects the wizard's Step-1 fields in slim form (name, slug, role, goal & behavior brief, model) with defaults for the rest. The final action calls the atomic-birth API (provider + starter agent + workspace in one request); a validation failure returns the user to the failed step with all entered data intact.

#### Scenario: Provider step previews models pre-birth
- **WHEN** the provider step collects a key and requests models
- **THEN** the model dropdown populates from the credential-preview endpoint without creating anything

#### Scenario: Birth success path
- **WHEN** the user completes the flow and confirms
- **THEN** the workspace is created with its provider config and starter agent (prompts_status generating); the user lands on the starter agent's chat

#### Scenario: Birth failure keeps user input
- **WHEN** the birth API returns 400 (e.g. invalid starter-agent slug)
- **THEN** the user stays on the failed step, a toast names the field error, and all entered data is preserved

## ADDED Requirements

### Requirement: Zero-membership state
A signed-in user with zero workspace memberships SHALL land on a dedicated state that explains workspaces are created by an instance administrator, offers a "request a workspace" affordance (mailto or admin contact copy — no server-side request exists), and stays out of the way of superadmins, who keep the creation entry. The state SHALL NOT offer the onboarding create flow to non-admins and SHALL NOT render workspace-scoped navigation.

#### Scenario: Member with no memberships
- **WHEN** a non-admin user with zero memberships finishes sign-in
- **THEN** the app shows the ask-your-admin state and no workspace-creation entry

#### Scenario: Superadmin path unchanged
- **WHEN** an instance administrator with zero memberships signs in
- **THEN** the creation entry is available and creating a workspace lands them in it as Owner
