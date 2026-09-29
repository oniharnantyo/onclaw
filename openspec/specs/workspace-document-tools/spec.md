# workspace-document-tools Specification

## Purpose

The `document.*` tool family: reading office documents (pdf, docx, xlsx, pptx, html, csv) to markdown on demand through `document.read`, and generating deliverables through `document.create` — both in-process in pure Go, with optional user-provided templates delivered by chat, capability-URL delivery, and the shared family seam (catalog, permission key, tool card, hooks) that future document verbs register against.

## Requirements

### Requirement: Document read tool
The runtime SHALL expose a built-in tool `document.read` whose input is a single file path with optional scope parameters, and whose output is the document's content converted to markdown text. Without scope parameters the tool SHALL return the full converted content as before. The tool SHALL accept any document format the converter registry registers — pdf, docx, xlsx, pptx, html, csv included — and SHALL convert documents in-process using pure-Go converters bounded by a conversion deadline, without shell or subprocess execution. The tool SHALL be available to agents subject to the standard tool denylist and workspace tool gate. Two scope parameters SHALL be supported: `pages`, a page range honored for PDF (whose converter emits page anchors), returning only the requested pages under the same output cap; and `section`, a heading, slide, or sheet title honored for structured formats, returning only the matched section. A `section` value matching no heading SHALL return a structured not-found result naming the document; a `pages` parameter on a non-PDF document SHALL return a structured note naming the limitation and the `section` alternative. Scoped-read failures SHALL NOT fail the agent run, and the tool SHALL remain registered and selectable.

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
### Requirement: Document read output contract
The `document.read` tool result SHALL be the converted markdown text capped at a configured maximum with an explicit truncation notice when the cap applies. When the converter produces no extractable text — a scanned or image-only document — the tool SHALL return a distinct no-text result naming the document, so the model can report the limitation rather than inventing content. The result SHALL NOT include converter internals beyond the document content and notices.

#### Scenario: Large document truncates with a notice
- **WHEN** a document's converted text exceeds the output cap
- **THEN** the tool result contains the truncated text followed by a notice stating the truncation

#### Scenario: Scanned PDF reports emptiness
- **WHEN** `document.read` executes on an image-only PDF whose text layer is empty
- **THEN** the tool returns a distinct no-extractable-text result naming the document

### Requirement: Document read conversion failure
When a document cannot be converted — corrupt, encrypted, or an uploaded format the converter registry does not register — `document.read` SHALL return a structured error result naming the document and the failure — the agent run SHALL NOT fail, and the tool SHALL remain registered and selectable. There is no conversion runtime to provision: conversion runs inside the server binary.

#### Scenario: Corrupt document returns a structured error
- **WHEN** `document.read` executes on a file whose bytes do not parse as its format
- **THEN** the tool result is a conversion-failure error naming the document, and the run continues

#### Scenario: Tool is always selectable
- **WHEN** an agent has not disabled `document.read` on any deployment
- **THEN** the tool resolves at composition — no availability gate or provisioning state exists
### Requirement: Document tool family seam
Document tools SHALL register as a family sharing one plumbing set: catalog entries with display name, description, group, and icon key per verb; a per-verb permission key subject to the workspace tool gate; a tool-card rendering rule; and a hook target addressable by name. Adding a sibling verb (e.g. a creation verb) SHALL require only a new registration against this family — no new plumbing. Adding a new document FORMAT SHALL require only one converter registration into the format registry — no tool changes.

#### Scenario: Family registration is sufficient for a new verb
- **WHEN** a future document verb registers with the family metadata
- **THEN** it appears in the catalog, is gateable, renders a tool card, and is hook-addressable without further backend changes

#### Scenario: Format registration is sufficient for a new format
- **WHEN** a new document format registers a converter into the format registry
- **THEN** `document.read` accepts files of that format with no changes to the tool, catalog, or gate

#### Scenario: document.read renders a tool card
- **WHEN** an agent turn includes a `document.read` call
- **THEN** the transcript tool card renders with a readable verb, the document name, and latency

### Requirement: Document create tool
The runtime SHALL expose a built-in tool `document.create` whose input is an output path, a format (`xlsx`, `pdf`, `docx`, `pptx`), and format-specific structured data, and whose output is the generated file written inside the agent workspace. The output path SHALL resolve through the same jail write contract as `write_file`. Each format SHALL generate from structured input: markdown for docx; slide structures for pptx; sheet/row structures for xlsx; a source mode for pdf (see the PDF routes requirement). Conversion or generation failures SHALL return structured per-document error results — the agent run SHALL NOT fail.

#### Scenario: Agent generates an invoice PDF
- **WHEN** an agent invokes `document.create` with format `pdf`, structured invoice data, and a workspace output path
- **THEN** the file is generated inside the workspace and the tool result names the document

#### Scenario: Agent builds a deck
- **WHEN** an agent invokes `document.create` with format `pptx` and a slide structure
- **THEN** the generated deck opens with the supplied slide count and text content

#### Scenario: Output path escape rejected
- **WHEN** `document.create` is invoked with an output path outside the jail's writable roots
- **THEN** the tool returns a parameter error and no file is written

### Requirement: Document create PDF routes
The `pdf` format SHALL support two source modes: agent-authored HTML rendered via headless Chrome (rod), and structured invoice data rendered by a pure-Go PDF generator. The structured route SHALL be available on every deployment; when Chrome is absent, the HTML route SHALL return a structured error naming the limitation and pointing at the structured route, and the run SHALL continue.

#### Scenario: HTML route renders
- **WHEN** `document.create` receives `pdf` with an HTML source on a deployment with Chrome available
- **THEN** the rendered PDF is written to the output path

#### Scenario: HTML route degrades without Chrome
- **WHEN** the HTML source mode is used on a deployment without Chrome
- **THEN** the tool returns a structured error naming the limitation and the structured route, and the run continues

#### Scenario: Structured route always works
- **WHEN** `document.create` receives `pdf` with structured invoice data on any deployment
- **THEN** the PDF is generated without external dependencies

### Requirement: Document create templates
The tool SHALL accept an optional `template` path parameter that switches generation to template-fill. A template path SHALL be a jail-validated path: the run's drop-lane attachment mount (a template sent in chat) or an ordinary workspace file. Template-fill SHALL be supported for xlsx (cell/range fill), docx, and pptx (placeholder text replacement, including placeholders split across XML runs). PDF SHALL NOT support template-fill — a PDF template request SHALL be rejected with guidance naming xlsx or docx templates instead. Without a template, generation SHALL use the format's default generator.

#### Scenario: Chat-attached template is usable
- **WHEN** a user attaches an xlsx template in chat and the agent invokes `document.create` with the template's run-scoped mount path
- **THEN** the tool fills the template and writes the result to the output path

#### Scenario: Split-run placeholders still fill
- **WHEN** a docx or pptx template contains a placeholder whose text is split across XML runs
- **THEN** the tool merges the runs and fills the placeholder

#### Scenario: PDF template rejected with guidance
- **WHEN** `document.create` receives format `pdf` with a template path
- **THEN** the tool returns a structured error suggesting an xlsx or docx template, or the structured HTML route

### Requirement: Document create delivery
After writing the generated file, the tool SHALL copy it into workspace storage, attach the capability URL to the tool result, and the transcript tool card SHALL render a download link from that URL — reusing the attachment-serving machinery without a new download endpoint.

#### Scenario: Created document is downloadable from the transcript
- **WHEN** a `document.create` call completes successfully
- **THEN** the transcript tool card exposes a download link serving the created file's bytes via its capability URL

### Requirement: Document create family registration
`document.create` SHALL register through the existing `document.*` family seam — catalog entry with display name, description, group, and icon key; a per-verb permission key subject to the workspace tool gate; a creation-verb tool-card rendering rule; and a hook target addressable by name — requiring no new plumbing beyond the registration.

#### Scenario: Registration is sufficient
- **WHEN** `document.create` registers with the family metadata
- **THEN** it appears in the catalog, is gateable, renders a tool card with a creation verb, document name, and latency, and is hook-addressable without further backend changes
