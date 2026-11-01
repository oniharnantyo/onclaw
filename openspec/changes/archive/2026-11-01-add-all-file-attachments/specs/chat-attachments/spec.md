# Spec Delta

## MODIFIED Requirements

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
