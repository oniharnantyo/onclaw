## Purpose

The `document.*` tool family lets agents read (and, in later verbs, create) office documents: `document.read` converts a document file — pdf, docx, xlsx, pptx, html, csv — to markdown on demand, in-process in pure Go through a per-format converter registry, so content reaches the model only when needed, through one uniform path on every provider, with no external conversion runtime to install.

## ADDED Requirements

### Requirement: Document read tool
The runtime SHALL expose a built-in tool `document.read` whose input is a single file path and whose output is the document's content converted to markdown text. The tool SHALL accept any document format the converter registry registers — pdf, docx, xlsx, pptx, html, csv included — and SHALL convert documents in-process using pure-Go converters bounded by a conversion deadline, without shell or subprocess execution. The tool SHALL be available to agents subject to the standard allowlist and workspace tool gate.

#### Scenario: Agent reads an attached docx
- **WHEN** an agent invokes `document.read` on the workspace path of an attached docx file
- **THEN** the tool result carries the document's content as markdown text including the document name

#### Scenario: Agent reads an attached pptx
- **WHEN** an agent invokes `document.read` on an attached pptx file
- **THEN** the tool result carries the slide content converted to markdown

#### Scenario: PDF is read like any other document
- **WHEN** an agent invokes `document.read` on an attached PDF
- **THEN** the tool result carries the PDF's text content as markdown

### Requirement: Document read path validation
The `document.read` tool SHALL accept only paths within the jailed filesystem the agent already has access to — the run's drop-lane attachment mounts and the agent workspace tree — and SHALL reject paths outside that boundary as a tool-parameter error, under the same contract as the filesystem read tools.

#### Scenario: Path traversal rejected
- **WHEN** an agent invokes `document.read` with a path outside the jail (e.g. a parent-directory escape)
- **THEN** the tool returns a parameter error and no file is read

#### Scenario: Drop-lane mount is readable
- **WHEN** an agent invokes `document.read` on the run-scoped read-only mount of an attached document
- **THEN** the tool reads the file and returns its converted content

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
- **WHEN** an agent's allowlist includes `document.read` on any deployment
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
