## ADDED Requirements

### Requirement: Context limit exposure
Catalog model entries SHALL carry the models.dev context limit (`limit.context`) for the model when the catalog publishes one; entries without a published limit SHALL omit it. Compatible provider types (`openai-compatible`, `anthropic-compatible`) SHALL NOT resolve catalog context limits, matching the existing provider-type mapping rule. The agent create/update flow SHALL consume this value to auto-fill an omitted agent `context_window` (see the agents capability).

#### Scenario: Catalog model with a published limit
- **WHEN** the catalog is consulted for a model models.dev publishes with `limit.context`
- **THEN** the entry exposes that limit in tokens

#### Scenario: Catalog model without a limit
- **WHEN** the catalog knows a model but publishes no `limit.context` for it
- **THEN** the entry omits the limit and the agent flow leaves `context_window` unset (runtime default applies)

#### Scenario: Compatible types skip the limit
- **WHEN** the catalog is consulted for an `openai-compatible` config
- **THEN** no catalog context limit is resolved for the model
