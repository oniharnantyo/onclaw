# chat-attachments Specification

## Purpose

Attach files and images to chat messages: select them from the composer, store them locally behind unguessable capability URLs, deliver them to the model as parts of the user message, and degrade gracefully when the selected model cannot read them.

## Requirements

### Requirement: Attachment selection and limits

The composer SHALL accept multiple files through an attach button, drag-and-drop onto the chat view, and pasted images. Per message the user MAY attach at most 4 files; images MUST be at most 5 MB, PDFs at most 20 MB, videos at most 20 MB, and inline text files at most 1 MB. Files outside the limits or of an unsupported type MUST be rejected with the reason shown on the attachment chip, and sending MUST be blocked while any attachment is still uploading.

#### Scenario: Attach an image

- **WHEN** the user attaches an image file in the composer
- **THEN** a chip shows upload progress and becomes ready, and the send control becomes available once the upload completes

#### Scenario: Reject an oversized file

- **WHEN** the user attaches an image larger than the size cap
- **THEN** the chip shows a rejected state with the size-limit reason and the message cannot be sent with that attachment

#### Scenario: Send blocked during upload

- **WHEN** the user presses send while an attachment chip is still uploading
- **THEN** the send is not dispatched until the upload finishes or the chip is removed

#### Scenario: Attach a video

- **WHEN** the user attaches a video file within the size cap
- **THEN** a chip shows upload progress and becomes ready like any other file

#### Scenario: Reject an oversized video

- **WHEN** the user attaches a video larger than the video size cap
- **THEN** the chip shows a rejected state with the size-limit reason and the message cannot be sent with that attachment

### Requirement: Capability-gated storage

Uploaded attachment files SHALL be stored under the application home on the local disk, named by an opaque random capability key rather than the original filename. This store is canonical: the file SHALL be retrievable only through its capability URL — a raw filesystem path MUST NOT be exposed to or rendered by the frontend, whose attachment links reference the capability URL only. In server mode with a bearer token configured, fetching an attachment SHALL require the token like every other HTTP surface. Stored attachments MUST remain retrievable across app restarts.

Every attachment that rides a sent message SHALL additionally be mirrored into the session agent's workspace at `uploads/` — host path `workspaces/<agent-slug>/uploads/`, model-visible `/workspace/uploads/<original name>` — when the turn that carries it is assembled. The mirror SHALL copy-if-absent from the canonical store, keep the original filename, and resolve name collisions with a numeric suffix. A mirror failure MUST NOT fail the turn: the degraded reference note falls back to the capability key alone. The model MAY be told the workspace-relative mirror path in a reference note; this is the only place a filesystem path for an attachment may appear, and the frontend surface is unchanged.

#### Scenario: Attachment survives restart

- **WHEN** the user attaches a file, sends it, and restarts the app
- **THEN** the attachment is still retrievable through the same capability URL shown in the transcript

#### Scenario: No raw path exposure

- **WHEN** the transcript renders an attachment
- **THEN** its download and preview links reference the capability URL only, never the on-disk path

#### Scenario: Upload lands in the workspace

- **WHEN** a turn carries an attachment for an agent whose workspace has no such file under `uploads/`
- **THEN** the file exists at `workspaces/<agent-slug>/uploads/<original name>` before the model input is built, and the agent's filesystem tools can read it at `/workspace/uploads/<original name>`

#### Scenario: Mirror name collision

- **WHEN** an attachment whose filename already exists in the agent's `uploads/` is mirrored
- **THEN** the copy lands under a numeric-suffixed name and the reference note addresses the suffixed path

#### Scenario: Mirror failure does not fail the turn

- **WHEN** the workspace mirror cannot be written for an attachment
- **THEN** the turn still runs and the reference note falls back to the attachment key without a workspace path

### Requirement: Multi-part user messages

A sent message with attachments SHALL persist the attachment metadata (name, type, size, capability URL) with the session's user-message event so replay reconstructs it. The transcript SHALL render image attachments as inline thumbnails and other attachments as icon chips with name, type, and size; selecting a chip opens or downloads the file through its capability URL. The turn's model input SHALL carry the attachment content as parts of the user message alongside the text. A message with attachments and no text MUST still be sendable.

#### Scenario: Transcript replay includes attachments

- **WHEN** the user sends a message with an image and later reloads the transcript
- **THEN** the image thumbnail renders from the persisted metadata

#### Scenario: Attachment-only message

- **WHEN** the user sends only a PDF with no typed text
- **THEN** the message sends and appears in the transcript as a user turn carrying the document chip

### Requirement: Model capability degradation

Every model SHALL advertise whether it accepts image, PDF, and video input. An attached kind SHALL be delivered to the model as a content part of the user message when the session's effective model advertises the kind AND the provider family's connector can carry that kind — image parts on every family, PDF file parts on the gemini and anthropic families, and video parts on the gemini family; the model's advertised capability remains the per-model gate, and for a compatible endpoint it is the user's assertion that the endpoint accepts the part. In every other case the turn SHALL deliver the attachment as a reference pointer instead of content bytes: the pointer names the true reason — that the model cannot read the kind, or that the provider's connector cannot carry it — and addresses the file at its workspace path (`/workspace/uploads/<name>`) so the agent can read it with its filesystem tools, including the capability key as a fallback when no mirror exists. The composer MUST warn before sending whenever a part will degrade, whether from a model gap or a connector gap. Sending MUST NOT fail solely because a model lacks an input capability or a connector lacks carriage.

#### Scenario: Image with a text-only model

- **WHEN** the user attaches an image while the session's model has no image input
- **THEN** the composer shows a capability warning, the send still succeeds, and the model receives a reference note instead of image bytes

#### Scenario: Capable model receives content

- **WHEN** the user attaches an image while the session's model accepts image input
- **THEN** the model receives the image content as part of the user message

#### Scenario: Video to a video-capable model

- **WHEN** the user attaches a video while the session's model advertises video input and its connector carries video parts
- **THEN** the model receives the video content as a video part of the user message

#### Scenario: Video to a model without video input

- **WHEN** the user attaches a video while the session's model does not advertise video input, or its connector cannot carry video parts
- **THEN** the composer shows a capability warning, the send still succeeds, and the model receives a reference note instead of video bytes

#### Scenario: PDF to an anthropic-family model with PDF input

- **WHEN** the user attaches a PDF while the session's model advertises PDF input and the provider is an anthropic-family connector
- **THEN** the model receives the PDF as a document part of the user message and no degradation note is produced

#### Scenario: PDF on a connector that cannot carry it

- **WHEN** the user attaches a PDF while the session's model advertises PDF input but the provider family's connector cannot carry PDF parts
- **THEN** the send succeeds and the model receives a reference note naming the connector gap and addressing the file at `/workspace/uploads/<name>`

#### Scenario: Composer warns on a connector gap

- **WHEN** the user attaches a PDF while the session's model advertises PDF input but the provider family cannot carry PDF parts
- **THEN** the composer shows a capability warning before sending, distinct from the model-capability warning
