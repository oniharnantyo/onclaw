# Spec Delta

## Purpose

Attach files and images to chat messages: select them from the composer, store them locally behind unguessable capability URLs, deliver them to the model as parts of the user message, and degrade gracefully when the selected model cannot read them.

## ADDED Requirements

### Requirement: Attachment selection and limits

The composer SHALL accept multiple files through an attach button, drag-and-drop onto the chat view, and pasted images. Per message the user MAY attach at most 4 files; images MUST be at most 5 MB, PDFs at most 20 MB, and inline text files at most 1 MB. Files outside the limits or of an unsupported type MUST be rejected with the reason shown on the attachment chip, and sending MUST be blocked while any attachment is still uploading.

#### Scenario: Attach an image

- **WHEN** the user attaches an image file in the composer
- **THEN** a chip shows upload progress and becomes ready, and the send control becomes available once the upload completes

#### Scenario: Reject an oversized file

- **WHEN** the user attaches an image larger than the size cap
- **THEN** the chip shows a rejected state with the size-limit reason and the message cannot be sent with that attachment

#### Scenario: Send blocked during upload

- **WHEN** the user presses send while an attachment chip is still uploading
- **THEN** the send is not dispatched until the upload finishes or the chip is removed

### Requirement: Capability-gated storage

Uploaded attachment files SHALL be stored under the application home on the local disk, named by an opaque random capability key rather than the original filename. The file SHALL be retrievable only through its capability URL — a raw filesystem path MUST NOT be exposed to the frontend or rendered in the transcript. In server mode with a bearer token configured, fetching an attachment SHALL require the token like every other HTTP surface. Stored attachments MUST remain retrievable across app restarts.

#### Scenario: Attachment survives restart

- **WHEN** the user attaches a file, sends it, and restarts the app
- **THEN** the attachment is still retrievable through the same capability URL shown in the transcript

#### Scenario: No raw path exposure

- **WHEN** the transcript renders an attachment
- **THEN** its download and preview links reference the capability URL only, never the on-disk path

### Requirement: Multi-part user messages

A sent message with attachments SHALL persist the attachment metadata (name, type, size, capability URL) with the session's user-message event so replay reconstructs it. The transcript SHALL render image attachments as inline thumbnails and other attachments as icon chips with name, type, and size; selecting a chip opens or downloads the file through its capability URL. The turn's model input SHALL carry the attachment content as parts of the user message alongside the text. A message with attachments and no text MUST still be sendable.

#### Scenario: Transcript replay includes attachments

- **WHEN** the user sends a message with an image and later reloads the transcript
- **THEN** the image thumbnail renders from the persisted metadata

#### Scenario: Attachment-only message

- **WHEN** the user sends only a PDF with no typed text
- **THEN** the message sends and appears in the transcript as a user turn carrying the document chip

### Requirement: Model capability degradation

Every model SHALL advertise whether it accepts image and PDF input. When the session's effective model cannot read an attached kind, the composer MUST warn before sending, and the turn SHALL deliver the attachment to the model as a reference pointer (name and capability link) instead of content bytes, so the agent can still mention the file but is never sent input the model cannot process. Sending MUST NOT fail solely because the model lacks image or PDF input.

#### Scenario: Image with a text-only model

- **WHEN** the user attaches an image while the session's model has no image input
- **THEN** the composer shows a capability warning, the send still succeeds, and the model receives a reference note instead of image bytes

#### Scenario: Capable model receives content

- **WHEN** the user attaches an image while the session's model accepts image input
- **THEN** the model receives the image content as part of the user message