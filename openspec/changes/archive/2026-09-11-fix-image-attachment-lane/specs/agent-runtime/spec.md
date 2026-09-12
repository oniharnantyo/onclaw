## ADDED Requirements

### Requirement: Model-bound attachment blocks carry bytes, not fetchable references
When the runtime builds a multimodal user message for the model, inline-image and inline-PDF attachment blocks SHALL carry the attachment's inline bytes and MIME type and SHALL NOT carry a server-relative capability URL in the reference field, because providers cannot resolve server-local URLs. The capability URL SHALL remain available to the transcript projection through the attachment block metadata, so hydrated attachment pills keep their download references.

#### Scenario: Image block reaches the provider as data
- **WHEN** a turn with an inline-image attachment builds its model message on a vision-capable model
- **THEN** the image block carries the attachment's base64 bytes and MIME type and no server-relative URL, and the provider accepts the block (the run does not fail on the image)

#### Scenario: Persisted transcript keeps the download reference
- **WHEN** the same turn is persisted and later reloaded in the transcript UI
- **THEN** the attachment pill still exposes the capability-URL download link, sourced from the block metadata

### Requirement: Model-modality attachment degradation
When building a turn's model message, the runtime SHALL resolve whether the turn's (provider, model) accepts each inline attachment input kind (image, PDF) and, for an input the model does not accept, SHALL replace that block with a pointer note naming the file, its type and size, and stating that the current model cannot view it — the run SHALL succeed. Capability is resolved per (provider, model), never by model name alone, and is tri-state: supported, unsupported, or unknown. Unknown capability SHALL fail open (the block is sent as-is).

#### Scenario: Image degrades on a text-only model
- **WHEN** an agent whose model accepts text-only input executes a turn carrying an inline-image attachment
- **THEN** the model message contains a pointer note for the image (name, type, size, cannot-view notice) instead of an image block, and the run completes successfully

#### Scenario: Image is sent whole on a vision-capable model
- **WHEN** an agent whose model accepts image input executes the same turn
- **THEN** the model message contains the image block with inline bytes and no pointer note

#### Scenario: Regeneration after a model switch sees the image again
- **WHEN** a turn that degraded on a text-only model is regenerated after the agent was reconfigured to a vision-capable model
- **THEN** the rebuilt model message contains the image block with inline bytes

#### Scenario: Unknown capability fails open
- **WHEN** the turn's (provider, model) has no catalog entry and no hint, and the turn carries an inline-image attachment
- **THEN** the image block is sent as-is (today's wire behavior), not degraded
