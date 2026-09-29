# Spec Delta

## MODIFIED Requirements

### Requirement: Document read tool
The runtime SHALL expose a built-in tool `document.read` whose input is a single file path with optional scope parameters, and whose output is the document's content converted to markdown text. Without scope parameters the tool SHALL return the full converted content as before. The tool SHALL accept any document format the converter registry registers — pdf, docx, xlsx, pptx, html, csv included — and SHALL convert documents in-process using pure-Go converters bounded by a conversion deadline, without shell or subprocess execution. The tool SHALL be available to agents subject to the standard allowlist and workspace tool gate. Two scope parameters SHALL be supported: `pages`, a page range honored for PDF (whose converter emits page anchors), returning only the requested pages under the same output cap; and `section`, a heading, slide, or sheet title honored for structured formats, returning only the matched section. A `section` value matching no heading SHALL return a structured not-found result naming the document; a `pages` parameter on a non-PDF document SHALL return a structured note naming the limitation and the `section` alternative. Scoped-read failures SHALL NOT fail the agent run, and the tool SHALL remain registered and selectable.

#### Scenario: Agent reads an attached docx
- **WHEN** an agent invokes `document.read` on the workspace path of an attached docx file
- **THEN** the tool result carries the document's content as markdown text including the document name

#### Scenario: Agent reads an attached pptx
- **WHEN** an agent invokes `document.read` on an attached pptx file
- **THEN** the tool result carries the slide content converted to markdown

#### Scenario: PDF is read like any other document
- **WHEN** an agent invokes `document.read` on an attached PDF
- **THEN** the tool result carries the PDF's text content as markdown

#### Scenario: Page-scoped PDF read
- **WHEN** an agent invokes `document.read` on a reference PDF with `pages` set to 30 through 34
- **THEN** the tool result carries only those pages' content with the page positions preserved, under the same output cap

#### Scenario: Section-scoped sheet read
- **WHEN** an agent invokes `document.read` on a reference workbook with `section` set to a sheet named `RateLimits`
- **THEN** the tool result carries only that sheet's content converted to markdown

#### Scenario: Unknown section is a structured result
- **WHEN** an agent invokes `document.read` with a `section` title that matches no heading in the document
- **THEN** the tool returns a structured not-found result naming the document, and the run continues

#### Scenario: Pages on a non-PDF names the alternative
- **WHEN** an agent invokes `document.read` with `pages` on a docx reference document
- **THEN** the tool returns a structured note that page scoping applies to PDFs only, naming the `section` parameter as the alternative

### Requirement: Document read path validation
The `document.read` tool SHALL accept only paths within the jailed filesystem the agent already has access to — the run's drop-lane attachment mounts, the agent workspace tree, and the run's read-only reference-documents mount — and SHALL reject paths outside that boundary as a tool-parameter error, under the same contract as the filesystem read tools. The references mount SHALL be composed per run and SHALL contain only the reference documents visible to that run under the workspace-reference-documents visibility rules; a reference document not visible to the run SHALL be unresolvable through the mount, indistinguishable from an unknown path. Files under the references mount SHALL be read-only: write and delete tools SHALL reject those paths.

#### Scenario: Path traversal rejected
- **WHEN** an agent invokes `document.read` with a path outside the jail (e.g. a parent-directory escape)
- **THEN** the tool returns a parameter error and no file is read

#### Scenario: Drop-lane mount is readable
- **WHEN** an agent invokes `document.read` on the run-scoped read-only mount of an attached document
- **THEN** the tool reads the file and returns its converted content

#### Scenario: References mount carries only visible documents
- **WHEN** a document is attached to `#incidents` only, and the agent invokes `document.read` on its references path from a direct chat
- **THEN** the path resolves as not-found, while the same invocation inside a `#incidents` run succeeds

#### Scenario: References mount rejects writes
- **WHEN** an agent invokes the write or delete tool on a path under the references mount
- **THEN** the tool rejects the operation as read-only, and the stored document is unchanged
