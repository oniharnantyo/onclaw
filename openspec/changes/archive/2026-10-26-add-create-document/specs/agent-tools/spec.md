# Spec Delta

## ADDED Requirements

### Requirement: Create document tool in the catalog

The built-in tool catalog SHALL include a `create_document` tool (display name "Create Document") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The tool is workspace-scoped: an agent without a rootable workspace SHALL have it withheld. The agent create/edit allowlist chips and the Tools pane card list SHALL list it without bespoke wiring.

#### Scenario: Create document appears in the catalog surfaces

- **WHEN** the Tools pane or the agent tool allowlist is rendered
- **THEN** a "Create Document" entry is listed alongside the other built-in tools, with a configuration action on its Tools pane card

#### Scenario: Disabled or unallowlisted create document is not offered

- **WHEN** the tool is disabled in the Tools pane, or an agent's allowlist omits it
- **THEN** that agent's turns are offered no `create_document` tool

#### Scenario: Slugless agent has the tool withheld

- **WHEN** an agent without a rootable workspace receives a turn
- **THEN** the `create_document` tool is withheld and the turn does not fail

### Requirement: Create document tool

The `create_document` tool SHALL render markdown into a PDF document using an embedded rendering library (no external binary) and write the result into the agent's workspace. Input SHALL be exactly the markdown content, a target file name, and an optional preset name. The file name SHALL be a plain name written at the workspace root; directory components, path traversal, and names resolving outside the workspace SHALL return an error result with no file written. The result SHALL carry the written file's workspace-relative name and size. A named preset that does not exist SHALL return a readable error with no file written. A call without a preset name SHALL use the configured default preset, falling back to the rendering library's built-in defaults when none is configured. Rendering failures, including a conversion exceeding its timeout, SHALL be returned as readable tool errors rather than failing the turn. Regenerating to an existing file name SHALL overwrite it.

#### Scenario: Markdown renders to a workspace PDF

- **WHEN** the model calls create_document with markdown content and file name "q3-report.pdf"
- **THEN** a PDF exists at that workspace-relative path and the tool result carries the name and size

#### Scenario: Unknown preset is a readable error

- **WHEN** the model names a preset that does not exist
- **THEN** the tool returns an error result naming the miss, nothing is written, and the turn continues

#### Scenario: File name cannot escape the workspace

- **WHEN** the model passes a file name carrying directory components or resolving outside the workspace
- **THEN** the tool returns an out-of-scope error result and no file is written

#### Scenario: Conversion failure is a readable error

- **WHEN** the rendering library fails or the conversion exceeds its timeout
- **THEN** the tool returns a readable error result and the turn continues

#### Scenario: Regenerating overwrites the earlier file

- **WHEN** the model calls create_document with a file name that already exists in the workspace
- **THEN** the new document replaces the old file and the result reports the fresh size
