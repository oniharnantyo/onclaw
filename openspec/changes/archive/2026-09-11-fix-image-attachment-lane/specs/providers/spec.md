## ADDED Requirements

### Requirement: Catalog mapping hint for compatible gateways
An `openai-compatible` or `anthropic-compatible` provider configuration SHALL accept an optional catalog-mapping hint identifying which community-catalog provider the gateway corresponds to (e.g. a models.dev provider id). When present, the hint SHALL be used wherever the provider type alone is insufficient for catalog resolution (model lists, effort values, input-modality resolution). Known gateway hosts MAY pre-fill the hint, but the user's explicit selection SHALL always win. The hint SHALL be optional — an absent hint leaves resolution exactly as it behaves without mapping (unknown).

#### Scenario: Hint unlocks catalog resolution
- **WHEN** an openai-compatible provider config carries a catalog-mapping hint for a gateway known to the community catalog
- **THEN** catalog-backed resolution (models, effort values, input modalities) uses that gateway's catalog entries

#### Scenario: No hint keeps unknown semantics
- **WHEN** an openai-compatible provider config carries no catalog-mapping hint and the host is not in the built-in hint map
- **THEN** catalog resolution treats the provider as unmapped (unknown), and nothing else about the provider changes
