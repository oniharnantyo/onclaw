## MODIFIED Requirements

### Requirement: Onboarding screen
A workspace with zero agents SHALL present the onboarding screen: workspace-ready headline, explanation that a workspace is a real container now (backed by the API) but empty until it has agents, a primary "Deploy your first agent" action opening the two-step agent wizard, a secondary path to workspace settings, and a timezone footer without plan text.

#### Scenario: Deploy first agent from onboarding
- **WHEN** the user clicks "Deploy your first agent" and completes the wizard
- **THEN** the new agent's chat opens and onboarding no longer appears for this workspace

#### Scenario: Timezone footer without plan
- **WHEN** the onboarding footer renders
- **THEN** the footer shows the timezone only — no plan text

## ADDED Requirements

### Requirement: Workspace creation flow
The workspace-creation flow SHALL add a provider step and an optional starter-agent step before birth: the provider step collects type, name, base_url (when the type requires it), and the API key, previews models via the credential-preview endpoint for the model choice, and the starter-agent step collects the wizard's Step-1 fields in slim form (name, slug, role, goal & behavior brief, model) with defaults for the rest. The final action calls the atomic-birth API (provider + starter agent + workspace in one request); a validation failure returns the user to the failed step with all entered data intact.

#### Scenario: Provider step previews models pre-birth
- **WHEN** the provider step collects a key and requests models
- **THEN** the model dropdown populates from the credential-preview endpoint without creating anything

#### Scenario: Birth success path
- **WHEN** the user completes the flow and confirms
- **THEN** the workspace is created with its provider config and starter agent (prompts_status generating); the user lands on the starter agent's chat

#### Scenario: Birth failure keeps user input
- **WHEN** the birth API returns 400 (e.g. invalid starter-agent slug)
- **THEN** the user stays on the failed step, a toast names the field error, and all entered data is preserved
