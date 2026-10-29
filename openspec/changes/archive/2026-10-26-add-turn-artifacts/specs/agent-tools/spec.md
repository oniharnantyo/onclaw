# Spec Delta

## MODIFIED Requirements

### Requirement: Create document tool

The `create_document` tool SHALL render markdown into a PDF document using an embedded rendering library (no external binary) and write the result into the agent's workspace under its `artifacts/` directory, creating that directory on demand. Input SHALL be exactly the markdown content, a target file name, and an optional preset name. The file name SHALL be a plain name; directory components, path traversal, and names resolving outside `artifacts/` SHALL return an error result with no file written. The result SHALL carry the written file's plain name and size. The tool's model-facing description SHALL state that output lands under `artifacts/`. A named preset that does not exist SHALL return a readable error with no file written. A call without a preset name SHALL use the configured default preset, falling back to the rendering library's built-in defaults when none is configured. Rendering failures, including a conversion exceeding its timeout, SHALL be returned as readable tool errors rather than failing the turn. Regenerating to an existing file name SHALL overwrite it.

#### Scenario: Markdown renders to a workspace PDF

- **WHEN** the model calls create_document with markdown content and file name "q3-report.pdf"
- **THEN** a PDF exists at `artifacts/q3-report.pdf` inside the workspace and the tool result carries the plain name and size

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

- **WHEN** the model calls create_document with a file name that already exists under `artifacts/`
- **THEN** the new document replaces the old file and the result reports the fresh size
