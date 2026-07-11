## ADDED Requirements

### Requirement: The web agent-edit form preserves all agent fields across a save

The web agent-edit page SHALL round-trip every persisted agent field unchanged when the user saves, regardless of whether model metadata has finished loading. In particular, a stored `reasoning_effort` and `reasoning_budget_tokens` SHALL be retained unless the user explicitly changes them, and the form SHALL NOT clear those fields as a side effect of a client-side model-capability guess. Reasoning capability SHALL be determined solely from the live model metadata returned by `/api/providers/{name}/models` (`thinking` flag and `reasoning_options`); the UI SHALL NOT infer reasoning support from the model's name (no provider-specific name regexes or prefixes), matching the `agent-update` requirement. When the selected model changes, the form SHALL re-resolve and store that model's discovered metadata into `model_metadata` so the persisted row reflects the chosen model (default metadata for a custom/unknown model), matching the `agent-update` requirement.

#### Scenario: Reasoning effort survives an edit on a non-OpenAI reasoning model

- **WHEN** the user edits an agent whose model is a reasoning model whose name does not start with `o1` or `o3` and whose `reasoning_effort` is `high`, and saves without touching the reasoning field
- **THEN** `GET /api/agents/{name}` still returns `reasoning_effort: high` (the value is not wiped while model metadata loads)

#### Scenario: Reasoning support is not inferred from the model name

- **WHEN** the agent's model is a reasoning model and the `/api/providers/{name}/models` response flags it `thinking: true`
- **THEN** the reasoning controls are shown and the stored reasoning value is preserved, with no fallback to a name-prefix check

#### Scenario: Changing the model refreshes stored metadata

- **WHEN** the user changes the agent's model to a discovered reasoning model and saves
- **THEN** `GET /api/agents/{name}` returns `model_metadata` with `thinking: true` and the Agents list card shows the reasoning badge

#### Scenario: A custom model is stored with default metadata

- **WHEN** the user types a model name absent from enumeration and saves
- **THEN** the agent is persisted with default metadata (context window 0, thinking false, text-only) and no error is shown

### Requirement: The agent workspace is editable from the web UI

The agent-edit Overview form SHALL expose `workspace` as a structured text field with a label, tooltip, and inline hint, and SHALL persist it through the existing agent update endpoint. The field SHALL NOT be presented as a raw-JSON editor. An empty value SHALL be valid and SHALL resolve to the agent's default workspace per the `agent-workspace` resolution precedence.

#### Scenario: A workspace set in the UI is visible to the CLI

- **WHEN** the user enters a workspace path on the agent-edit page and saves
- **THEN** `onclaw agent show <name>` reports that workspace

#### Scenario: An empty workspace resolves to the agent default

- **WHEN** the user leaves the workspace field empty and saves
- **THEN** the stored `workspace` is empty and the agent resolves its default workspace (`~/.onclaw/workspace/<agent>/`)

#### Scenario: An existing workspace round-trips through an edit

- **WHEN** the user opens an agent that already has a workspace and saves the Overview form without changing it
- **THEN** the workspace value is unchanged after the save
