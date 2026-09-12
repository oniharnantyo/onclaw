## ADDED Requirements

### Requirement: Input-modality resolution
The model catalog SHALL resolve, for a (provider, model) pair, whether the model accepts non-text input kinds — at minimum image and PDF — as a tri-state result: supported, unsupported, or unknown. Resolution SHALL be provider-scoped (the same model id on different gateways may expose different modalities) and SHALL draw on the community catalog's per-model input-modality and attachment-attachment metadata, cached under the existing catalog refresh cycle. An unmapped provider type without a caller-supplied catalog mapping SHALL resolve to unknown.

#### Scenario: Vision-capable gateway entry
- **WHEN** the catalog lists the provider's model with image in its input modalities and image-input resolution is requested
- **THEN** the result is supported

#### Scenario: Same model id, text-only gateway
- **WHEN** a different gateway's catalog entry for the same model id lists text-only input
- **THEN** image-input resolution for that (provider, model) is unsupported — resolution follows the provider mapping, not the model name

#### Scenario: Unmapped provider resolves unknown
- **WHEN** input-modality resolution is requested for an openai-compatible provider with no catalog mapping supplied
- **THEN** the result is unknown rather than supported or unsupported
