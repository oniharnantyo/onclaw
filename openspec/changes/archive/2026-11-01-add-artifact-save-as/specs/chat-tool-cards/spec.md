## ADDED Requirements

### Requirement: Artifact export

A `create_document` tool card SHALL offer a Save As control for the generated document in every format the tool produces. The export SHALL resolve the source exactly like the card's view read — a plain file name with no directory components, resolved inside the calling agent's workspace `artifacts/` directory first and at the workspace root when absent, refused before touching the filesystem otherwise. The destination SHALL be chosen through the host's native save dialog, seeded with the artifact's file name; confirming the dialog copies the file to the chosen destination and the UI reports the outcome, while cancelling it is a silent no-op. The chosen destination path is the user's own dialog selection and MAY be reported to the UI; the workspace's on-disk paths remain internal as before. On a host without a native dialog service the export SHALL be reported unavailable and the card SHALL NOT offer the control.

#### Scenario: Save As exports the generated document

- **WHEN** the user picks Save As on a generated document's card and confirms a destination in the native dialog
- **THEN** the file is copied to the chosen destination and the UI confirms the export

#### Scenario: Export resolves the jailed source

- **WHEN** an export is requested for a plain file name that exists under the calling agent's workspace `artifacts/` directory
- **THEN** the copy sources that file, and the same request for a name with directory components or traversal is refused before any filesystem access

#### Scenario: Cancel is a silent no-op

- **WHEN** the user cancels the native save dialog
- **THEN** no copy happens and the UI shows nothing

#### Scenario: Host without dialogs

- **WHEN** the app runs on a host without a native dialog service and the card renders
- **THEN** the Save As control is not offered and an export attempt reports unavailable
