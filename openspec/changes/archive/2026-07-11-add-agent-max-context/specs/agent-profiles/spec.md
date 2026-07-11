## ADDED Requirements

### Requirement: An agent stores a per-agent maximum context window

The system SHALL store a per-agent `max_context_tokens` on the agent profile. A value of `0` SHALL mean "inherit." When assembling an agent, the system SHALL resolve the effective context window as the first available of: the agent's `max_context_tokens` when greater than zero, the global `max_context_tokens` config value when greater than zero, then the selected model's discovered context window. The per-agent value SHALL override the global config value, which SHALL override the model default. Existing agents SHALL default to `0` so current global-driven behavior is preserved.

#### Scenario: A per-agent override wins over the global value

- **WHEN** an agent has `max_context_tokens = 32000` and the global config is `64000`
- **THEN** the agent assembles with an effective context window of `32000`

#### Scenario: An unset agent inherits the global value

- **WHEN** an agent has `max_context_tokens = 0` and the global config is `64000`
- **THEN** the agent assembles with an effective context window of `64000` (unchanged from today)

#### Scenario: Both unset falls back to the model default

- **WHEN** an agent has `max_context_tokens = 0`, the global config is `0`/unset, and the model's discovered context window is `128000`
- **THEN** the agent assembles with an effective context window of `128000`

#### Scenario: Existing agents are unaffected by the migration

- **WHEN** the `agents.max_context_tokens` column is added
- **THEN** every existing row has `max_context_tokens = 0` and preserves its prior behavior
