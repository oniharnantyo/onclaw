## MODIFIED Requirements

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name and timezone through the API on explicit save (default model and thread retention remain local workspace fields until their domains integrate; the workspace URL is display-only and shows no plan indicator). The timezone field SHALL be a searchable selector over the full IANA zone list, each entry showing its UTC offset.

#### Scenario: Save settings
- **WHEN** a member with workspace.write saves a new workspace name or timezone
- **THEN** the change persists (reload keeps it) and a toast confirms

#### Scenario: Save without permission
- **WHEN** a member without workspace.write attempts to save
- **THEN** the save action is unavailable (or rejected with a forbidden toast)

#### Scenario: Searchable timezone
- **WHEN** the user types "jakar" into the timezone field
- **THEN** "Asia/Jakarta" with its UTC offset is selectable
