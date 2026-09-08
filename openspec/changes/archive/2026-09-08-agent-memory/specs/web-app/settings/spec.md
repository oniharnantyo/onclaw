## ADDED Requirements

### Requirement: Workspace memory editor
The Workspace pane SHALL include a shared-memory (`WORKSPACE.md`) editor below the existing workspace fields: a free-form textarea (memory is unstructured markdown), a live size/token counter fed by the server cap, and save via the workspace memory endpoint. The editor SHALL load for all members (read state) and save SHALL follow the workspace settings-management permission — Members see the editor disabled or read-only, Owner/Admin persist changes. Over-cap saves SHALL surface the 422 field error inline. The editor SHALL NOT participate in the general workspace-details save (renaming the workspace must not rewrite memory and vice versa).

#### Scenario: Admin edits shared memory
- **WHEN** an Owner or Admin opens Settings → Workspace, edits the memory textarea, and saves
- **THEN** the content persists via the workspace memory endpoint without altering the workspace's other fields

#### Scenario: Member sees read-only
- **WHEN** a Member opens the Workspace pane
- **THEN** the memory editor is visible read-only and its save control is disabled or hidden

#### Scenario: Rename does not touch memory
- **WHEN** an Admin saves a workspace rename from the pane
- **THEN** the stored memory content is unchanged
