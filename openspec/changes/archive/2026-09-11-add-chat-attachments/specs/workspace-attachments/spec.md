## Purpose

Chat attachments: uploading user files (images, documents) for agent chats with validation, per-workspace storage, capability-URL serving, and the three-lane format classification that decides how each file reaches the model. Uploads happen out-of-band so turn requests and persisted history carry references, never bytes.

## ADDED Requirements

### Requirement: Attachment upload
The system SHALL provide an authenticated workspace-scoped upload endpoint accepting multipart form-data with a single `file` field. Uploads SHALL require workspace-member authentication; the attachment SHALL be recorded against the key's workspace and creator. On success the endpoint SHALL return the attachment's id, original filename, detected media type, byte size, and a capability URL.

#### Scenario: Successful upload
- **WHEN** a workspace member uploads a 1.2 MB PNG via multipart form-data
- **THEN** the response is `201` with `{id, name, mime, size, url}`, the mime is the server-detected type, and `url` is a capability URL serving the stored bytes

#### Scenario: Unauthenticated upload
- **WHEN** the upload endpoint is called without workspace-member credentials
- **THEN** the request is rejected with an authentication error and no attachment record is created

### Requirement: Upload validation
The system SHALL determine the actual content type from magic bytes, never from the client-declared type, and SHALL enforce a per-type size cap and a per-message attachment count cap: images (png, jpeg, webp, gif) up to 5 MB; PDF up to 20 MB and approximately 100 pages; text-like files up to 50 MB in the drop lane. Zero-byte uploads and empty filenames SHALL be rejected.

#### Scenario: Claimed type overridden by sniffing
- **WHEN** a client uploads an executable renamed to `image.png`
- **THEN** magic-byte detection identifies the real type and the upload is rejected as a disallowed type

#### Scenario: Oversize image rejected at upload
- **WHEN** an 8 MB PNG is uploaded
- **THEN** the endpoint responds `413` with a message naming the 5 MB image cap, before any turn is sent

#### Scenario: Zero-byte upload rejected
- **WHEN** a zero-byte file is uploaded
- **THEN** the endpoint responds `400` and stores nothing

### Requirement: Three-lane format classification
Each attachment SHALL be classified into exactly one lane: the **inline lane** (image types and PDF, delivered to the model as native image/file blocks; text-like files at or under 200 KB, delivered as fenced text), the **drop lane** (text-like files over 200 KB up to 50 MB — code, config, logs — stored and made readable to the agent's file tools without entering model context wholesale), or the **reject lane** (office formats docx/xlsx/pptx, archives, executables, and unknown types, which SHALL be rejected at upload with a message suggesting PDF export). Rejection SHALL occur at upload time, not at turn time.

#### Scenario: Office format rejected with guidance
- **WHEN** a user uploads `report.docx`
- **THEN** the upload is rejected with a message that office formats are unsupported and suggests exporting as PDF

#### Scenario: Text-like file enters the drop lane
- **WHEN** a 4 MB SQL dump is uploaded
- **THEN** the upload succeeds and the attachment is classified as drop-lane, readable through the agent's file tools

#### Scenario: Small text file enters the inline lane
- **WHEN** a 40 KB `.yaml` config is uploaded
- **THEN** the upload succeeds and the attachment is classified as inline-lane text, delivered inside the turn message as a fenced text part

#### Scenario: Oversize PDF rejected
- **WHEN** a 40 MB PDF is uploaded
- **THEN** the upload is rejected with the PDF size cap stated

### Requirement: Attachment tenancy
An attachment SHALL be reachable only within its workspace: resolving an attachment id from a different workspace SHALL fail not-found, indistinguishable from an unknown id, without revealing existence.

#### Scenario: Foreign attachment unresolvable
- **WHEN** a turn or fetch references an attachment id belonging to another workspace
- **THEN** the resolution fails not-found with the same shape as an unknown id

### Requirement: Capability-URL serving
Stored attachments SHALL be served through unguessable capability URLs (128-bit random keys), the same mechanism as avatar files. The capability URL is the attachment's wire token: the web client renders previews and downloads from it, and `/v1` input parts reference attachments by it.

#### Scenario: Browser renders thumbnail from capability URL
- **WHEN** a transcript renders a user message carrying an uploaded image
- **THEN** the thumbnail loads from the attachment's capability URL without additional authentication

### Requirement: Pluggable attachment blob storage
Attachment blobs SHALL be stored through the storage port's driver abstraction: local filesystem storage (the instance default, zero-configuration) and an S3-compatible driver (endpoint, region, bucket, access key, secret, optional path-style for MinIO/R2-class stores) SHALL both be available, registered as ordinary drivers — no attachment code path may special-case a driver. Attachment records SHALL record which backend holds each blob, so blobs written under a previous configuration remain readable after the workspace switches backends. Capability URLs SHALL remain onclaw-proxied regardless of driver: the browser and wire clients always fetch through onclaw, which streams from the configured backend; drivers never change URL semantics.

#### Scenario: Same upload flow on both drivers
- **WHEN** a workspace uploads the same image under the local driver and then, after switching configuration, under the S3-compatible driver
- **THEN** both uploads return the same response shape with a capability URL, and both thumbnails render identically through onclaw

#### Scenario: Switching backends preserves old attachments
- **WHEN** a workspace switches attachment storage from local to S3-compatible and a transcript referencing a locally-stored attachment is reloaded
- **THEN** the old attachment still resolves and renders, because its record names the backend that holds it

#### Scenario: S3 outage surfaces as upload failure
- **WHEN** the configured S3 backend is unreachable and a user uploads a file
- **THEN** the upload fails with a storage error and the composer chip shows the failed state; local workspaces are unaffected

### Requirement: Workspace storage configuration
Each workspace SHALL configure its blob storage backend through the workspace Storage pane in settings (chat attachments being this configuration's first consumer), defaulting to the instance's local storage when unconfigured. Configuration SHALL include the driver choice and, for S3-compatible drivers, endpoint, region, bucket, access key id, and secret access key (persisted encrypted; returned masked with a keep-stored sentinel). Saving S3 configuration SHALL first verify connectivity to the bucket; a failed verification SHALL reject the save with the probe's reason. Settings management permission (workspace Owner/Admin) SHALL be required to view masked configuration or change it.

#### Scenario: Unconfigured workspace uses local storage
- **WHEN** a workspace has never saved attachment storage configuration and a user uploads an attachment
- **THEN** the blob lands in the instance's local storage and everything behaves as if no configurability existed

#### Scenario: Save gated on connectivity
- **WHEN** an Owner saves S3 configuration with a wrong secret
- **THEN** the save is rejected with the connectivity probe's failure reason, and the previous configuration remains active

#### Scenario: Secret masking on read
- **WHEN** the storage configuration is fetched for display
- **THEN** the secret access key returns as a masked sentinel, and saving without changing it keeps the stored secret

### Requirement: Drop-lane materialization is driver-agnostic
Drop-lane attachments SHALL be made readable to the agent's file tools by materializing them into a run-scoped local directory mounted read-only into the jail, regardless of which blob backend holds the bytes; the materialized directory SHALL be cleaned up when the run tears down. Local-driver uploads MAY serve the mount directly, but the agent-visible path and behavior SHALL not depend on the configured backend.

#### Scenario: Drop-lane file readable under S3 storage
- **WHEN** a workspace stores attachments in S3 and a turn carries a drop-lane SQL dump
- **THEN** the runner materializes the file into the run-scoped directory and the model reads it through the filesystem tools during the turn

#### Scenario: Materialization cleaned up
- **WHEN** a run with materialized drop-lane files finishes
- **THEN** the run-scoped directory is removed and no drop-lane bytes linger outside the configured backend


### Requirement: Orphan acceptance
Attachments uploaded but never referenced by a sent turn SHALL be tolerated; garbage collection of orphaned uploads is explicitly deferred.

#### Scenario: Abandoned upload is harmless
- **WHEN** a client uploads a file and never sends a turn referencing it
- **THEN** the attachment record and bytes remain stored without affecting any turn or session
