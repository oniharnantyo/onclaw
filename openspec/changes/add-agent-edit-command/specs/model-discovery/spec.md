## MODIFIED Requirements

### Requirement: The models.dev catalog is parsed from its documented JSON shape

The system SHALL parse the `models.dev` catalog as a JSON object keyed directly by provider id (there is no wrapper field), where each provider object contains a `models` map keyed by model id. For each model, the system SHALL read the context window from `limit.context`, the thinking/reasoning flag from `reasoning`, the input modalities from `modalities.input`, and the reasoning controls from `reasoning_options`. Each `reasoning_options` entry SHALL be read as one of three types: `effort` with its `values` array, `budget_tokens` with its `min` and `max`, or `toggle`. A root-shape or field-path mismatch SHALL NOT silently leave the catalog empty; parsing SHALL be verified against a real-shaped payload.

#### Scenario: A real-shaped models.dev payload populates the catalog

- **WHEN** the catalog parser is given a payload whose root is `{"openai":{"models":{"gpt-4o":{"reasoning":false,"limit":{"context":128000},"modalities":{"input":["text","image"]}}}}}`
- **THEN** the parsed catalog yields provider `openai`, model `gpt-4o` with context window 128000, thinking false, and input modalities `["text","image"]`

#### Scenario: An effort model's reasoning options are parsed

- **WHEN** the catalog parser is given a model entry with `"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","medium","high"]}]`
- **THEN** the parsed model records `reasoning` true and a reasoning option of type `effort` with values `["low","medium","high"]`

#### Scenario: A budget-tokens model's reasoning options are parsed

- **WHEN** the catalog parser is given a model entry with `"reasoning":true,"reasoning_options":[{"type":"budget_tokens","min":128,"max":32768}]`
- **THEN** the parsed model records a reasoning option of type `budget_tokens` with min 128 and max 32768

#### Scenario: A toggle model's reasoning options are parsed

- **WHEN** the catalog parser is given a model entry with `"reasoning":true,"reasoning_options":[{"type":"toggle"}]`
- **THEN** the parsed model records a reasoning option of type `toggle` with no values or range

#### Scenario: A model resolves from the catalog when /v1/models lacks the data

- **WHEN** a selected model's `/v1/models` entry provides no context window and the model exists in the parsed `models.dev` catalog
- **THEN** the resolver supplies the catalog's `limit.context`, `reasoning`, `modalities.input`, and `reasoning_options` for that model rather than defaulting to context window 0
