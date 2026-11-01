## Purpose

Attach files and images to chat messages: select them from the composer, store them locally behind unguessable capability URLs, deliver them to the model as parts of the user message, and degrade gracefully when the selected model cannot read them.

## Requirements

### Requirement: Attachment selection and limits

The composer SHALL accept multiple files through an attach button, drag-and-drop onto the chat view, and pasted images. The picker SHALL NOT filter by file type: any file the user selects or drops is an attachment candidate. Per message the user MAY attach at most 4 files. Each file MUST satisfy its lane's size cap: images at most 5 MB, PDFs at most 20 MB, videos at most 20 MB, inline text files at most 1 MB, OOXML office documents (`.docx`, `.xlsx`, `.pptx`) at most 20 MB, and every other file at most 20 MB (the catch-all file lane — an empty or unrecognized type takes this lane). A file over its lane's cap, or a message over the per-message cap, MUST be rejected with the reason shown on the attachment chip; no file type is rejected for being unsupported. Sending MUST be blocked while any attachment is still uploading.

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

#### Scenario: Attach any file type

- **WHEN** the user attaches a file whose type is none of image, PDF, video, text, or office document — for example a `.zip` archive or a file with no reported type — within the file lane's cap
- **THEN** a chip shows upload progress and becomes ready, and the message is sendable

#### Scenario: Reject an oversized file-lane attachment

- **WHEN** the user attaches a file outside the known lanes that exceeds 20 MB
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

Every model SHALL advertise whether it accepts image, PDF, and video input. An attached kind SHALL be delivered to the model as a content part of the user message when the session's effective model advertises the kind AND the provider family's connector can carry that kind — image parts on every family, PDF file parts on the gemini and anthropic families, and video parts on the gemini family; the model's advertised capability remains the per-model gate, and for a compatible endpoint it is the user's assertion that the endpoint accepts the part. In every other case the turn SHALL deliver the attachment as a reference pointer instead of content bytes: the pointer names the true reason — that the model cannot read the kind, or that the provider's connector cannot carry it — and addresses the file at its workspace path (`/workspace/uploads/<name>`) so the agent can read it with its filesystem tools, including the capability key as a fallback when no mirror exists. Office documents and file-lane attachments SHALL always be delivered as a reference pointer naming that their bytes are not sent, regardless of the model's advertised capabilities or the provider family — the pointer addresses the workspace path like every other pointer. The composer MUST warn before sending whenever a part that would otherwise ride the message will degrade, whether from a model gap or a connector gap; lanes that never ride by design (office documents and file-lane attachments) MUST NOT add a degrade warning. Sending MUST NOT fail solely because a model lacks an input capability or a connector lacks carriage.

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

#### Scenario: Office document always arrives as a reference

- **WHEN** the user attaches a `.docx`, `.xlsx`, or `.pptx` regardless of the session's model or provider family
- **THEN** the send succeeds, the composer shows no degrade warning for it, and the model receives a reference note stating the bytes are not sent and addressing the file at `/workspace/uploads/<name>`

#### Scenario: Arbitrary file always arrives as a reference

- **WHEN** the user attaches a file outside the image, PDF, video, text, and office lanes — for example a `.zip` archive — regardless of the session's model or provider family
- **THEN** the send succeeds, the composer shows no degrade warning for it, and the model receives a reference note stating the bytes are not sent and addressing the file at `/workspace/uploads/<name>`


### Requirement: Drag-and-drop attachment surface

Dragging files over the chat conversation surface (the transcript and composer column) SHALL surface a drag-over affordance — a highlight on the composer and a drop hint — that appears when a file drag enters the column and clears when it leaves or the drop completes. Dropping files onto the column SHALL attach them through the same acceptance limits, chip tray, upload gating, and degradation warnings as the attach button and paste, with each dropped file judged independently: one rejected file MUST NOT reject the others in the same drop. Drops on surfaces outside the chat column, and drags that carry no files (e.g. selected text), MUST attach nothing and MUST NOT navigate the application away from the chat — the app SHALL stay on the current view for every drop, inside or outside the column. Dropping text into the composer SHALL continue to insert text as it does today.

#### Scenario: Drop a file on the chat column

- **WHEN** the user drops a file within the chat column
- **THEN** a chip mints and uploads exactly as if the file had been picked, and the transcript and composer stay on the current session

#### Scenario: Drop multiple files with one rejected

- **WHEN** the user drops several files at once and one of them exceeds its size cap
- **THEN** the oversized file's chip shows a rejected state with the size-limit reason while the other files in the same drop attach and upload normally

#### Scenario: Drag-over affordance appears and clears

- **WHEN** the user drags a file over the chat column and then either moves out of it or drops
- **THEN** the affordance shows while the drag is over the column and is gone after leaving or dropping

#### Scenario: Drop outside the chat column never navigates

- **WHEN** the user drops a file on a surface outside the chat column (for example the right bar)
- **THEN** nothing attaches and the application remains on the chat view

#### Scenario: Non-file drag stays inert

- **WHEN** the user drags selected text (no files) over the chat column
- **THEN** no drag-over affordance shows and no attachment is created

#### Scenario: Text drop still inserts into the composer

- **WHEN** the user drops selected text onto the composer input
- **THEN** the text is inserted into the draft as before, and no attachment is created
