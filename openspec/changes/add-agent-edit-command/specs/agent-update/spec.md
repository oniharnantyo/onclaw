## ADDED Requirements

### Requirement: Agent configurations can be updated with partial fields

The system SHALL provide `onclaw agent edit <name>` that accepts optional flags for agent properties (provider, model, reasoning, reasoning-budget, workspace, system-prompt, tools, max-iterations). Only fields specified via flags SHALL be updated; unspecified fields SHALL retain their existing values. The system SHALL preserve the agent's `created_at` timestamp and automatically update `updated_at`. When the model changes, the system SHALL re-resolve and store the model's discovered metadata.

#### Scenario: Update model only

- **WHEN** user runs `onclaw agent edit coder --model glm-4.6`
- **THEN** the agent's model and model metadata are updated and all other fields remain unchanged

#### Scenario: Update multiple fields

- **WHEN** user runs `onclaw agent edit coder --model glm-4.6 --reasoning high --max-iterations 30`
- **THEN** the agent's model, model metadata, reasoning_effort, and max_iterations are updated and all other fields remain unchanged

### Requirement: Models are validated via discovery, not name patterns

The system SHALL resolve a model supplied via `--model` (or chosen in the picker) against the provider's enumerated models and the cached `models.dev` catalog. A model found in either source SHALL be accepted with its discovered metadata. A model found in neither SHALL still be accepted on explicit manual entry, with metadata resolved to defaults (context window 0, thinking false, text-only input). The system SHALL NOT apply provider-specific name regexes.

#### Scenario: A discovered model is accepted

- **WHEN** user runs `onclaw agent edit coder --provider openai --model gpt-5`
- **THEN** `gpt-5` is accepted and its discovered metadata (context window, thinking flag, modalities, reasoning options) is stored

#### Scenario: An unknown model is accepted with default metadata

- **WHEN** user runs `onclaw agent edit coder --model some-private-finetune` and the model is absent from both the provider API and the catalog
- **THEN** the model is accepted and stored with default metadata (context window 0, thinking false, text-only), without error

### Requirement: Reasoning effort is validated against the model's supported options

When a reasoning control is supplied, the system SHALL validate it strictly against the selected model's discovered `reasoning_options`: an `effort` value SHALL be one of the model's supported effort values; a `budget_tokens` value SHALL fall within the model's `[min, max]` range; a `toggle` control SHALL be `on` or `off`. The system SHALL reject any reasoning control on a model that is not a reasoning model, and SHALL reject any value the model does not support, failing with a message that lists the valid options.

#### Scenario: A supported effort value is accepted

- **WHEN** the selected model supports effort values `["low","medium","high"]` and the user runs `onclaw agent edit coder --reasoning high`
- **THEN** the agent's reasoning_effort is set to `high`

#### Scenario: An unsupported effort value is rejected

- **WHEN** the selected model supports effort values `["medium","high","xhigh"]` and the user runs `onclaw agent edit coder --reasoning low`
- **THEN** the command fails with an error listing `medium`, `high`, `xhigh` as the valid values

#### Scenario: A budget value outside the range is rejected

- **WHEN** the selected model supports budget_tokens `[1024, 32768]` and the user runs `onclaw agent edit coder --reasoning-budget 500`
- **THEN** the command fails with an error stating the valid range

#### Scenario: Effort on a non-reasoning model is rejected

- **WHEN** the selected model is not a reasoning model and the user runs `onclaw agent edit coder --reasoning high`
- **THEN** the command fails with an error stating the model is not a reasoning model

### Requirement: Reasoning effort is collected when a thinking model is selected

When the interactive model picker selects a reasoning model whose discovered metadata declares `reasoning_options`, the system SHALL prompt the user for the effort level using the model's primary control: an enum choice for `effort` models, an integer prompt bounded by `[min, max]` for `budget_tokens` models, and an on/off confirm for `toggle` models. The picker SHALL NOT prompt for effort when the selected model is not a reasoning model.

#### Scenario: An effort model prompts for the level

- **WHEN** the user selects a model supporting effort values `["low","medium","high"]` in the picker
- **THEN** the picker prompts the user to choose one of those values

#### Scenario: A budget-tokens model prompts for a token count

- **WHEN** the user selects a model supporting budget_tokens `[128, 32768]` in the picker
- **THEN** the picker prompts for an integer within that range

#### Scenario: A non-reasoning model does not prompt

- **WHEN** the user selects a non-reasoning model in the picker
- **THEN** no effort prompt is shown

### Requirement: Provider references are validated before updates

The system SHALL validate that any newly specified provider exists and is enabled before applying updates. Attempts to reference non-existent or disabled providers SHALL fail with a clear error message.

#### Scenario: Valid provider reference succeeds

- **WHEN** user runs `onclaw agent edit coder --provider anthropic`
- **THEN** the provider validation passes and the update is applied

#### Scenario: Invalid provider reference fails

- **WHEN** user runs `onclaw agent edit coder --provider nonexistent-provider`
- **THEN** the command fails with error "referenced provider 'nonexistent-provider' not found or disabled"

### Requirement: System prompt can be updated interactively

The system SHALL support updating an agent's system prompt via `--system-prompt` flag. When the value is `-`, the system SHALL read the prompt from standard input until EOF (Ctrl+D). Empty string values SHALL clear the system prompt.

#### Scenario: Update system prompt with flag value

- **WHEN** user runs `onclaw agent edit coder --system-prompt "You are a helpful assistant."`
- **THEN** the agent's system_prompt is updated to the specified string

#### Scenario: Update system prompt from stdin

- **WHEN** user runs `onclaw agent edit coder --system-prompt -` and provides input via stdin
- **THEN** the agent's system_prompt is updated with the stdin content

#### Scenario: Clear system prompt

- **WHEN** user runs `onclaw agent edit coder --system-prompt ""`
- **THEN** the agent's system_prompt is cleared to empty string

### Requirement: Hot-reload signals running processes after updates

The system SHALL trigger the hot-reload mechanism after successfully updating an agent configuration. Running onclaw processes SHALL receive a SIGHUP signal or equivalent notification to reload agent configurations from the database.

#### Scenario: Edit triggers hot-reload

- **WHEN** user runs `onclaw agent edit coder --model glm-4.6`
- **THEN** after the update succeeds, running onclaw processes are signaled to reload configurations

### Requirement: Edit operations maintain data integrity

The system SHALL validate that the agent exists before applying updates, and SHALL validate field values before persisting. Invalid values SHALL be rejected before any database write, leaving the database in a consistent state.

#### Scenario: Edit non-existent agent fails

- **WHEN** user runs `onclaw agent edit nonexistent-agent --model glm-4.6`
- **THEN** the command fails with error "agent 'nonexistent-agent' not found"

#### Scenario: Invalid reasoning value is rejected before saving

- **WHEN** user runs `onclaw agent edit coder --reasoning bogus` and the value is not supported by the selected model
- **THEN** the command fails before any database write, listing the model's valid reasoning options

#### Scenario: Database errors prevent partial updates

- **WHEN** a database error occurs during the update operation
- **THEN** no fields are modified and the command fails with a descriptive error
