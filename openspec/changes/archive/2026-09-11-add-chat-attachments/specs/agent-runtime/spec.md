## ADDED Requirements

### Requirement: Multimodal user turns
Agent chat turns SHALL accept an optional attachment set alongside the text input. When attachments are present, the turn's user message SHALL be constructed as a multimodal message carrying, in order: fenced text parts for inline-lane text attachments, native image blocks for inline-lane images, native file blocks for inline-lane PDFs, and — for drop-lane attachments — a pointer text note naming the file and its read-only workspace path so the agent can inspect it with file tools. Attachment-only messages (no user text) SHALL be valid turns. Cron and channel origins remain text-only in v1; command turns (compact) never carry attachments.

#### Scenario: Image turn reaches the model as bytes
- **WHEN** a user sends a message with text and one uploaded PNG attachment
- **THEN** the turn's user message carries the text and the image block with the image's bytes (base64 data), and the model's answer reflects the image content

#### Scenario: Attachment-only message
- **WHEN** a user sends a message with an attached screenshot and no text
- **THEN** the turn executes with a user message whose content is the image block alone, and it is billed and streamed like any turn

#### Scenario: Drop-lane pointer note
- **WHEN** a turn carries a 4 MB SQL dump attachment
- **THEN** the user message carries a pointer note giving the file's name and its read-only workspace path, and the model can read the file's contents through the filesystem tools during the same turn

#### Scenario: Compact never carries attachments
- **WHEN** a compact turn request includes attachments
- **THEN** the request fails validation before execution and no run starts

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

### Requirement: Attachment bytes never persist
Session event persistence SHALL NOT contain attachment bytes. When persisting a turn's user message, attachment blocks SHALL be demoted to reference form (capability URL, filename, media type, size); the same reference form is what history hydration reads. Session event payloads therefore stay lightweight regardless of attachment size.

#### Scenario: Session payload stays lean
- **WHEN** a turn with a 5 MB image attachment completes and its events are persisted
- **THEN** the persisted user message carries the attachment's reference metadata and URL, and no base64 payload appears in the session events

#### Scenario: Model replay after demotion
- **WHEN** the ADK replays persisted history for a later turn's model context
- **THEN** URL-only attachment blocks are replaced by placeholders per the current-turn-only policy, never re-sent as URLs

### Requirement: Transcript attachment fidelity
A session's persisted transcript SHALL surface user-message attachments: each completed user message that carried attachments SHALL project them as structured attachment metadata (name, media type, size, capability URL) in the transcript event, identical for the live stream and the hydrated read path, so a reloaded transcript renders the same attachment chips the live turn showed. Attachment pointer notes written for the model SHALL NOT leak into the projected message text.

#### Scenario: Hydrated transcript shows attachments
- **WHEN** a workspace member reloads a session whose earlier turn carried an image and a PDF
- **THEN** the projected user-message events carry both attachments' metadata with capability URLs, indistinguishable from what the live stream showed

#### Scenario: Pointer note hidden from transcript text
- **WHEN** a user message projected into the transcript carried a drop-lane pointer note for the model
- **THEN** the projected message text contains only the user's own text, not the pointer note
