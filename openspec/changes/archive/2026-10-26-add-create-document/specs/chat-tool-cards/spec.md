# Spec Delta

## ADDED Requirements

### Requirement: Generated document access

A `create_document` tool card SHALL present the generated PDF for viewing. The webview SHALL obtain a generated document's bytes only through a bound read addressed by the workspace-relative file name the tool reported. The read MUST accept only a plain file name with no directory components, MUST resolve it solely inside the calling agent's workspace directory, and MUST refuse any other name before touching the filesystem. The on-disk absolute path MUST NOT be exposed to the webview.

#### Scenario: Generated PDF renders on the card

- **WHEN** a create_document call succeeds
- **THEN** its card offers the generated PDF, fetched through the bound read by the reported file name

#### Scenario: Name with directory components is refused

- **WHEN** a card requests a name carrying directory components or traversing upward
- **THEN** the bound read refuses the name before touching the filesystem

#### Scenario: Missing file is a readable failure

- **WHEN** a card requests a name whose file no longer exists in the workspace
- **THEN** the read fails with a readable error and the card shows the failure
