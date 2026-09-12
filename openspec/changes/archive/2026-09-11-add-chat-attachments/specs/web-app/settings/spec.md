## ADDED Requirements

### Requirement: Storage pane
Workspace settings SHALL provide a Storage pane as the last entry of the settings navigation (route `/settings/storage`), visible read-only to Members and editable by Owners/Admins. The pane configures the workspace's blob storage backend — chat attachments are its first consumer — and SHALL show the active backend (Local when unconfigured, with an explanatory default note) and let an Owner/Admin choose between Local and S3-compatible storage. The S3 choice SHALL reveal structured fields — endpoint URL, region, bucket, access key id, secret access key (write-only, masked sentinel for a stored value), and a path-style toggle for MinIO/R2-class stores — each a labeled field, never a raw JSON textarea. Saving S3 configuration SHALL run a connectivity probe first and surface its outcome inline (success toast, or the failure reason with the previous configuration left active). Configuration changes SHALL apply to new uploads only; existing stored files stay readable.

#### Scenario: Default state shows local
- **WHEN** a workspace has no storage configuration
- **THEN** the pane shows Local storage as the active backend with a note that uploads use the instance data directory, and no S3 fields are shown

#### Scenario: S3 fields on driver selection
- **WHEN** an Owner selects "S3-compatible" as the driver
- **THEN** the endpoint, region, bucket, access key, secret, and path-style fields appear as labeled inputs

#### Scenario: Probe-gated save success
- **WHEN** an Owner saves S3 configuration and the connectivity probe passes
- **THEN** a toast confirms the save, the pane shows S3-compatible as active, and subsequent uploads land in the bucket

#### Scenario: Probe-gated save failure
- **WHEN** an Owner saves S3 configuration and the probe fails (e.g. wrong secret)
- **THEN** an inline error shows the probe's failure reason, the save does not take effect, and the previously active backend remains

#### Scenario: Stored secret never echoed
- **WHEN** the pane is reopened after S3 configuration was saved
- **THEN** the secret field shows the masked sentinel, and saving without touching it preserves the stored secret

#### Scenario: Member sees read-only pane
- **WHEN** a Member opens the Storage pane
- **THEN** the configuration is visible but the driver selector and fields are disabled (or the save action is unavailable with a forbidden toast)

#### Scenario: Pane sits last in the settings navigation
- **WHEN** the settings navigation renders
- **THEN** "Storage" appears as its last entry, after Notifications
