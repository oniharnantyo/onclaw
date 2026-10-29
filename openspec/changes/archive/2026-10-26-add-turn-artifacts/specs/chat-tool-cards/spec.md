# Spec Delta

## MODIFIED Requirements

### Requirement: Generated document access

A `create_document` tool card SHALL present the generated PDF for viewing. The webview SHALL obtain a generated document's bytes only through a bound read addressed by the plain file name the tool reported. The read MUST accept only a plain file name with no directory components, MUST resolve it inside the calling agent's workspace `artifacts/` directory first and, when no file exists there, at the workspace root — so cards recorded before generated documents moved under `artifacts/` keep rendering — and MUST refuse any other name before touching the filesystem. Both locations are inside the agent's workspace; nothing else is ever served. The on-disk absolute path MUST NOT be exposed to the webview.

#### Scenario: Generated PDF renders on the card

- **WHEN** a create_document call succeeds
- **THEN** its card offers the generated PDF, fetched through the bound read by the reported file name

#### Scenario: Legacy root document still renders

- **WHEN** a card requests a name whose file exists only at the workspace root, generated before the `artifacts/` directory was introduced
- **THEN** the bound read serves it from the root and the card renders the PDF

#### Scenario: Artifacts copy takes precedence

- **WHEN** a name exists both under `artifacts/` and at the workspace root
- **THEN** the bound read serves the `artifacts/` copy

#### Scenario: Name with directory components is refused

- **WHEN** a card requests a name carrying directory components or traversing upward
- **THEN** the bound read refuses the name before touching the filesystem

#### Scenario: Missing file is a readable failure

- **WHEN** a card requests a name whose file exists neither under `artifacts/` nor at the workspace root
- **THEN** the read fails with a readable error and the card shows the failure
