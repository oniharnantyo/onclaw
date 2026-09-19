## ADDED Requirements

### Requirement: Workspace default model inheritance
At run start the runner SHALL resolve the agent's effective provider and model in one place — the agent's pinned `(provider_id, model)` pair when both are set, otherwise the workspace default model pair. Every downstream composition decision for the run (credential resolution, catalog hint, input modality, model construction, context-window resolution for an agent without a stored `context_window`) SHALL use the effective pair. An inheriting agent whose workspace has no default model SHALL fail the run fast with an error naming the missing workspace setting — never a mid-run model error. When the effective provider's type requires a max-tokens value and the agent does not pin one (only possible for inheriting agents, whose save-time check cannot know the type), the runner SHALL apply its documented default max-tokens value instead of failing the run.

#### Scenario: Pinned agent unaffected
- **WHEN** a run starts for an agent with both provider and model set
- **THEN** the effective pair is the agent's own and resolution never consults the workspace default

#### Scenario: Inheriting agent runs the workspace default
- **WHEN** a run starts for an agent with the empty pair while the workspace default model is set
- **THEN** the run composes on the workspace default's provider credential and model id

#### Scenario: Inheriting agent without a default fails fast
- **WHEN** a run starts for an inheriting agent whose workspace default has been removed out-of-band
- **THEN** the run fails immediately with an error naming the missing workspace setting

#### Scenario: Run-time max-tokens default
- **WHEN** an inheriting agent runs on an effective provider type that requires max_tokens and the agent pins none
- **THEN** the run proceeds with the runner's default max-tokens value rather than erroring
