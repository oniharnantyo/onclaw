## MODIFIED Requirements

### Requirement: Agent configurations can be updated with partial fields

The system SHALL provide `onclaw agent edit <name>` that accepts optional flags for agent properties (provider, model, reasoning, reasoning-budget, workspace, system-prompt, tools, max-iterations, max-context). Only fields specified via flags SHALL be updated; unspecified fields SHALL retain their existing values. The system SHALL preserve the agent's `created_at` timestamp and automatically update `updated_at`. When the model changes, the system SHALL re-resolve and store the model's discovered metadata. A `--max-context` value of `0` SHALL clear the per-agent override so the agent inherits the global default; omitting the flag SHALL leave the stored value unchanged.

#### Scenario: Update model only

- **WHEN** user runs `onclaw agent edit coder --model glm-4.6`
- **THEN** the agent's model and model metadata are updated and all other fields remain unchanged

#### Scenario: Update multiple fields

- **WHEN** user runs `onclaw agent edit coder --model glm-4.6 --reasoning high --max-iterations 30`
- **THEN** the agent's model, model metadata, reasoning_effort, and max_iterations are updated and all other fields remain unchanged

#### Scenario: Set a per-agent max context override

- **WHEN** user runs `onclaw agent edit coder --max-context 32000`
- **THEN** the agent's `max_context_tokens` is set to `32000` and all other fields remain unchanged

#### Scenario: Clearing the override inherits the global default

- **WHEN** user runs `onclaw agent edit coder --max-context 0`
- **THEN** the agent's `max_context_tokens` is set to `0` and the agent assembles with the global default context window

#### Scenario: Omitting the flag preserves the stored value

- **WHEN** user runs `onclaw agent edit coder --system-prompt "…"` and `coder` already has `max_context_tokens = 32000`
- **THEN** `max_context_tokens` remains `32000`
