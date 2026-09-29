# Spec Delta

## MODIFIED Requirements

### Requirement: As-is blob storage
A reference document's original bytes SHALL be stored unmodified through the workspace's configured storage driver — the same per-workspace configuration the Storage settings pane controls and chat attachments honor (local by default, S3-compatible when configured) — never through an instance-level default that ignores workspace configuration. The driver that stored each blob SHALL be recorded on the document row, and every later read of that blob — capability-URL download, indexing and re-index conversion, runner mount materialization, and deletion — SHALL dispatch on the recorded backend so local and S3-backed documents coexist and remain byte-identical readable across driver switches. As a distinct blob kind from chat attachments, no transformed copy — converted markdown, extracted text, or normalized file — SHALL be stored as an agent-visible artifact. On re-upload (replace), the blob SHALL be written to the workspace's then-current driver, the recorded backend updated, and derived state rebuilt from the new bytes; on delete, the blob SHALL be removed from the backend recorded on the row, along with all derived state.

#### Scenario: Upload with workspace configured for S3
- **WHEN** a workspace's storage is configured for an S3-compatible bucket and a member uploads a reference document
- **THEN** the blob is stored in that bucket, the document row records the S3 backend, and the download via its capability URL returns the uploaded bytes byte-identical

#### Scenario: Mixed backends coexist across a driver switch
- **WHEN** a workspace has a document stored locally, then switches storage to S3 and uploads a second document
- **THEN** the first document still downloads byte-identical from local storage while the second is served from the bucket, with no migration of the first

#### Scenario: Agent mount reads from the recorded backend
- **WHEN** an agent run materializes its references mount for a document whose blob is stored on S3
- **THEN** the mounted file content matches the stored blob, fetched through the recorded backend

#### Scenario: Replace rebuilds derived state
- **WHEN** a member replaces `twilio-api.pdf` with a newer edition while the workspace is on S3
- **THEN** the new blob is stored under the workspace's current driver with its backend recorded, and the search index reflects the new content, with no leftover sections from the previous edition

#### Scenario: Delete removes from the recorded backend
- **WHEN** a member deletes a document whose recorded backend is S3
- **THEN** the blob is removed from the bucket and the document, its index, and its capability URL stop resolving
