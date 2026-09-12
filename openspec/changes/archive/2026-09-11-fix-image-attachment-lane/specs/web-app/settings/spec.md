## ADDED Requirements

### Requirement: Model combobox capability icons
The agent configuration model combobox SHALL render capability icons on each model option row — image input, PDF input, reasoning, and tool calling — using a distinct icon per capability (eye, file, brain, wrench respectively). An icon SHALL appear only when the catalog affirmatively supports that capability for the (provider, model); unsupported or unknown capabilities SHALL show no icon rather than a struck-through or dimmed state. Each icon SHALL carry a human-readable tooltip. The icons reuse the catalog data the combobox already loads for context-window autofill; no separate lookup is introduced.

#### Scenario: Icons reflect catalog capabilities
- **WHEN** the model dropdown opens for a provider whose catalog entries carry per-model input modalities, reasoning, and tool-calling flags
- **THEN** each option row shows exactly the icons for the capabilities that model affirmatively supports

#### Scenario: Unknown models stay quiet
- **WHEN** the dropdown includes a model with no catalog entry
- **THEN** that row shows the model name with no capability icons and no placeholder markers
