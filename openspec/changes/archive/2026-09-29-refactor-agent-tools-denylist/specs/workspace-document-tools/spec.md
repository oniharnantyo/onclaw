# Spec Delta — workspace-document-tools

## MODIFIED Requirements

### Requirement: Document read tool
The runtime SHALL expose a built-in tool `document.read` whose input is a single file path and whose output is the document's content converted to markdown text. The tool SHALL accept any document format the converter registry registers — pdf, docx, xlsx, pptx, html, csv included — and SHALL convert documents in-process using pure-Go converters bounded by a conversion deadline, without shell or subprocess execution. The tool SHALL be available to agents subject to the standard tool denylist and workspace tool gate.

#### Scenario: Agent reads an attached docx
- **WHEN** an agent invokes `document.read` on the workspace path of an attached docx file
- **THEN** the tool result carries the document's content as markdown text including the document name

#### Scenario: Agent reads an attached pptx
- **WHEN** an agent invokes `document.read` on an attached pptx file
- **THEN** the tool result carries the slide content converted to markdown

#### Scenario: PDF is read like any other document
- **WHEN** an agent invokes `document.read` on an attached PDF
- **THEN** the tool result carries the PDF's text content as markdown

### Requirement: Document read conversion failure
When a document cannot be converted — corrupt, encrypted, or an uploaded format the converter registry does not register — `document.read` SHALL return a structured error result naming the document and the failure — the agent run SHALL NOT fail, and the tool SHALL remain registered and selectable. There is no conversion runtime to provision: conversion runs inside the server binary.

#### Scenario: Corrupt document returns a structured error
- **WHEN** `document.read` executes on a file whose bytes do not parse as its format
- **THEN** the tool result is a conversion-failure error naming the document, and the run continues

#### Scenario: Tool is always selectable
- **WHEN** an agent has not disabled `document.read` on any deployment
- **THEN** the tool resolves at composition — no availability gate or provisioning state exists
