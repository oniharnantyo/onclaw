## MODIFIED Requirements

### Requirement: Model-bound attachment blocks carry bytes, not fetchable references
When the runtime builds a multimodal user message for the model, inline-image attachment blocks SHALL carry the attachment's inline bytes and MIME type and SHALL NOT carry a server-relative capability URL in the reference field, because providers cannot resolve server-local URLs. The capability URL SHALL remain available to the transcript projection through the attachment block metadata, so hydrated attachment pills keep their download references. Documents are not model-bound blocks: PDFs classify as drop-lane attachments and reach the model only as pointer notes naming the document read tool.

#### Scenario: Image block reaches the provider as data
- **WHEN** a turn with an inline-image attachment builds its model message on a vision-capable model
- **THEN** the image block carries the attachment's base64 bytes and MIME type and no server-relative URL, and the provider accepts the block (the run does not fail on the image)

#### Scenario: Persisted transcript keeps the download reference
- **WHEN** the same turn is persisted and later reloaded in the transcript UI
- **THEN** the attachment pill still exposes the capability-URL download link, sourced from the block metadata

### Requirement: Model-modality attachment degradation
When building a turn's model message, the runtime SHALL resolve whether the turn's (provider, model) accepts each inline attachment input kind (image) and, for an input the model does not accept, SHALL replace that block with a pointer note naming the file, its type and size, and stating that the current model cannot view it — the run SHALL succeed. Capability is resolved per (provider, model), never by model name alone, and is tri-state: supported, unsupported, or unknown. Unknown capability SHALL fail open (the block is sent as-is). The image lane is the only inline attachment kind subject to modality resolution: documents do not enter model context as blocks, so no connector or modality gating applies to them.

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

### Requirement: Attachment context lifetime is current-turn-only
Attachment bytes SHALL be delivered to the model only during the turn that carries them. On every subsequent model call within that turn (tool-loop iterations, retries) the attachments remain present. Persisted history SHALL carry attachment references, never bytes, and the model-time expansion SHALL replace reference-only blocks from older turns with placeholder text naming the attachment and its workspace path. The policy is a single model-time rule keyed on the block's own shape: blocks carrying bytes pass through; URL-only blocks become placeholders.

#### Scenario: Image visible across one turn's tool loop
- **WHEN** a turn with an attached screenshot invokes tools and makes multiple model calls before answering
- **THEN** every model call in that turn receives the image bytes

#### Scenario: Older attachments collapse to placeholders
- **WHEN** a later turn on the same session executes after a turn that carried an image
- **THEN** the model context shows a text placeholder for the older image (naming the file and its workspace path) instead of the image bytes, and token usage does not include the older image

#### Scenario: Regenerated turn re-expands its attachments
- **WHEN** a turn carrying attachments is regenerated
- **THEN** the regenerated run is the current turn for its attachments and delivers their bytes to the model without re-uploading

#### Scenario: Document attachments are never model-bound blocks
- **WHEN** a turn carries a PDF or office-format attachment on any provider
- **THEN** the model message contains the drop-lane pointer note naming the document read tool, not a file block, on every model call of the turn
