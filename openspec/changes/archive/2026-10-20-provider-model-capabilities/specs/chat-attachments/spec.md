# Spec Delta

## MODIFIED Requirements

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

### Requirement: Model capability degradation

Every model SHALL advertise whether it accepts image, PDF, and video input. When the session's effective model cannot read an attached kind, the composer MUST warn before sending, and the turn SHALL deliver the attachment to the model as a reference pointer (name and capability link) instead of content bytes, so the agent can still mention the file but is never sent input the model cannot process. A video attachment SHALL be delivered as a video content part when the model advertises video input and the provider's connector can carry video parts; otherwise it degrades like any unreadable kind. Sending MUST NOT fail solely because the model lacks image, PDF, or video input.

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
