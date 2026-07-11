## ADDED Requirements

### Requirement: The agent-edit form exposes a checkbox-gated max-context override

The web agent-edit Overview form SHALL expose the per-agent max-context override behind an "Override max context" checkbox. The checkbox state SHALL derive from the stored value — checked when `max_context_tokens > 0`, unchecked when it is `0`. When unchecked, the number input SHALL be disabled or hidden and the agent inherits the global default context window on save; when checked, the number input SHALL be enabled and accept a positive integer. Unchecking and saving SHALL store `max_context_tokens = 0` (inherit); checking and saving SHALL store the entered value. The checkbox maps to the existing `0 = inherit` sentinel and introduces no new storage field. The number field SHALL carry a label, tooltip, inline validation (positive integer), and a hint showing the global default, and MAY pre-fill with the selected model's discovered context window when the override is first enabled. The value SHALL be saved through the existing agent update endpoint as a typed integer (never a raw-JSON editor and never dropped due to undefined-omission).

#### Scenario: The override is off by default and inherits the global value

- **WHEN** the user opens an agent whose `max_context_tokens` is `0` (or creates a new agent)
- **THEN** the "Override max context" checkbox is unchecked, the number field is disabled or hidden, and saving leaves the agent inheriting the global default context window

#### Scenario: Checking the box enables the override

- **WHEN** the user checks "Override max context", enters `32000`, and saves
- **THEN** `onclaw agent show <name>` reports `max_context_tokens: 32000` and the agent assembles with a 32000-token context window

#### Scenario: A previously-set override re-opens checked and populated

- **WHEN** the user opens an agent with a stored `max_context_tokens` of `32000`
- **THEN** the checkbox renders checked and the number field shows `32000`; saving without changing it leaves the value unchanged

#### Scenario: Unchecking clears the override back to inherit

- **WHEN** the user unchecks "Override max context" on an agent whose override was `32000` and saves
- **THEN** the stored `max_context_tokens` becomes `0` and the agent assembles with the global default context window
