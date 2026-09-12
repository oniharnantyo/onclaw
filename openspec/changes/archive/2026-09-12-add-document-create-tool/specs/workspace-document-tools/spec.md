## Purpose

Extends the `document.*` family with the creation verb: `document.create` generates xlsx, pdf, docx, and pptx deliverables from structured model input, with optional user-provided templates delivered by chat, and returns a downloadable link through the transcript tool card.

## ADDED Requirements

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
