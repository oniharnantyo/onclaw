## ADDED Requirements

### Requirement: Generate-before-persist on create
Creating an agent SHALL generate identity/soul/bootstrap prompts synchronously **before** the agent row is persisted: the generation pipeline resolves the payload's provider config, invokes the configured model, parses the three documents, and writes them into the agent's workspace directory; only then is the row inserted — directly in `prompts_status: ready`. The create request path MAY block on the LLM call within the bounded generation timeout. A generation failure SHALL abort the create: response is 400 `invalid_request` with the sanitized provider error, no agent row is created, and the seeded workspace directory is removed. `generating` no longer occurs on the create path; it describes in-flight regeneration only.

#### Scenario: Create blocks while the model answers
- **WHEN** an agent is created with a valid provider config
- **THEN** the 201 response arrives after generation completes, carrying `prompts_status: ready` (documents populated)

#### Scenario: Generation failure aborts create
- **WHEN** the model call fails during agent creation
- **THEN** the response is 400 `invalid_request` with the sanitized generation error, no agent row exists, and the agent's workspace directory is removed

#### Scenario: No failed agents from create
- **WHEN** agents are created through the endpoint
- **THEN** no agent enters the workspace in `prompts_status: failed` from the create path; `failed` arises only from regeneration or the interrupted-generation sweep

## REMOVED Requirements

### Requirement: Synchronous generation on create
**Reason**: The create path no longer persists-then-generates; generation now gates persistence, so the old requirement's failure semantics (create returns 201 with a persisted, retryable `failed` agent) are inverted by design.
**Migration**: Behavior is replaced by "Generate-before-persist on create"; regeneration remains the retry path for live agents, and the deploy wizard's error state + toast replaces the roster Retry affordance for failed deploys.
