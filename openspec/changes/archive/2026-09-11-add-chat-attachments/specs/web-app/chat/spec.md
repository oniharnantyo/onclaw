## ADDED Requirements

### Requirement: Attachment composer tray
The agent-chat composer SHALL provide an attachment tray between the message text area and the input controls, fed by three entry paths: the attach button's file picker, clipboard paste of file items into the text area, and drag-and-drop of files onto the chat surface. Each attached file renders as a chip showing an image thumbnail or a document icon, the filename, and the size. Chips SHALL progress through states: uploading (with progress indicator and cancel), ready (with remove), rejected (with the server's reason inline, e.g. size cap exceeded or "export as PDF" for office formats), and failed (with retry — re-uploading the retained file — and remove). Pasted clipboard files without names SHALL be named with a timestamped default. A drag-in-progress SHALL show a dashed drop-target overlay over the message list; drops SHALL be prevented from navigating the browser. Folders and over-cap selections SHALL be rejected with explicit toasts. The tray is per-conversation local state: switching conversations clears it and aborts in-flight uploads.

#### Scenario: Chip lifecycle on a slow upload
- **WHEN** a user picks a 4.8 MB PDF and it uploads over a slow connection
- **THEN** a chip appears immediately in the uploading state with a progress indicator and a cancel control, and becomes a ready chip when the upload completes

#### Scenario: Failed upload offers retry
- **WHEN** an upload fails with a network error mid-flight
- **THEN** the chip shows a failed state with Retry and Remove, and Retry re-uploads the same file without re-picking

#### Scenario: Rejected at the door
- **WHEN** a user attaches `report.docx`
- **THEN** a rejected chip appears naming the reason and the "export as PDF" guidance, and the composer's send state treats it as absent

#### Scenario: Paste a screenshot
- **WHEN** a user presses paste with an image on the clipboard while the composer is focused
- **THEN** an uploading chip appears with a timestamped default name, and the pasted text (if any) is unaffected

#### Scenario: Drag files onto the chat
- **WHEN** a user drags two PNG files over the chat surface
- **THEN** a dashed overlay appears reading "Drop to attach", and dropping adds both files as uploading chips (subject to the per-message cap); dragging text does not show the overlay

#### Scenario: Session switch clears the tray
- **WHEN** the user switches conversations while an upload is in flight
- **THEN** the tray empties and the in-flight upload is aborted

### Requirement: Attachment send gate
Sending SHALL be blocked while any tray chip is uploading. The send control SHALL be enabled when the text is non-empty or at least one chip is ready — attachment-only messages are valid. Attachments MUST never be silently dropped on send: a message that sends carries exactly the ready chips. Regeneration re-sends chip references (ids), not re-uploads.

#### Scenario: Enter mid-upload does not send
- **WHEN** a user presses Enter while a chip is still uploading
- **THEN** nothing sends; the send control is visibly disabled until the chip resolves

#### Scenario: Attachment-only send
- **WHEN** the tray holds a ready image chip and the text area is empty
- **THEN** send is enabled and sends a message whose visible content is the attachment alone

#### Scenario: Optimistic bubble matches hydration
- **WHEN** a message with attachments is sent
- **THEN** the optimistic user bubble renders the same chips (thumbnail/icon, name, size) that a page reload renders from hydrated history

### Requirement: Transcript attachment rendering
User messages in the transcript — live and hydrated alike — SHALL render attachments below the message text: images as inline thumbnails loaded from their capability URLs, documents as an icon chip with filename, media type, size, and a download link. Rejected states never render in the transcript (rejection lives only in the composer). Drop-lane attachments render identically to documents; the pointer note is not user-visible text.

#### Scenario: Image message rendering
- **WHEN** a user message carries one PNG attachment
- **THEN** the bubble shows the message text followed by an inline thumbnail of the image

#### Scenario: Document chip rendering
- **WHEN** a user message carries a PDF attachment
- **THEN** the bubble shows a document chip with the filename, "PDF · 4.8 MB" sizing, and a download affordance

#### Scenario: Reload renders the same chips
- **WHEN** a session with an attachment message is reloaded from history
- **THEN** the user bubble renders the same chips as it did live, sourced from the hydrated attachment metadata
