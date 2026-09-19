## MODIFIED Requirements

### Requirement: Workspace settings
PATCH /workspaces/:ws SHALL require the `workspace.write` permission for name/timezone/description updates and for setting or clearing the workspace default model. The default model SHALL be stored as a `(default_provider_id, default_model)` pair governed by both-or-neither semantics: both set pins the default, both empty (or both absent from the payload) leaves it unset; a half-set pair SHALL be 400 invalid_request, and a set pair SHALL reference a provider config in the same workspace (unknown provider SHALL be 400 invalid_request). Clearing the default SHALL be refused with 422 while any agent in the workspace inherits it (empty provider/model pair), the refusal naming the agent count. Workspace read payloads (including the switcher/boot payload) SHALL carry the default model as `{provider_id, model}` so clients can offer it for agent inheritance.

#### Scenario: Authorized update
- **WHEN** a member with workspace.write updates name, timezone, or description
- **THEN** response is 200, workspace updated, slug untouched

#### Scenario: Set default model
- **WHEN** a member with workspace.write PATCHes `default_model: {provider_id, model}` naming a workspace provider
- **THEN** response is 200 and the pair persists on the workspace

#### Scenario: Half-set pair rejected
- **WHEN** a PATCH carries a model with no provider id (or the reverse)
- **THEN** response is 400 invalid_request

#### Scenario: Unknown provider rejected
- **WHEN** a PATCH sets `default_model` naming a provider id that does not exist in the workspace
- **THEN** response is 400 invalid_request

#### Scenario: Clearing blocked while inherited
- **WHEN** a PATCH clears `default_model` while agents with an empty provider/model pair exist in the workspace
- **THEN** response is 422 naming the inherit-agent count, and the stored default is unchanged

#### Scenario: Payload carries the default model
- **WHEN** a client reads the workspace through the switcher/boot payload
- **THEN** the payload includes `default_model` (`null` when unset)

#### Scenario: Unauthorized update
- **WHEN** a member without workspace.write PATCHes the workspace
- **THEN** response is 403

#### Scenario: First-run seeding (see instance-admin)
- **WHEN** a fresh instance seeds its first superadmin from the environment
- **THEN** the superadmin's home tenant is the default master tenant, not a customer workspace
