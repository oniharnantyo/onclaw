# Spec Delta

## Purpose

Lets agents read, revise, and produce real-world office documents with the original's formatting intact: see DOCX/XLSX/PPTX content, edit workspace files in place with targeted operations through one editing tool (office revisions and PDF structural operations), guided by a seeded `documents` playbook skill.

## ADDED Requirements

### Requirement: Reading office documents

The jailed filesystem read path SHALL present office documents as model-readable views: DOCX and PPTX as structured text views (paragraphs, headings, slide text in document order) and XLSX as a cell-grid view (sheet names with cell addresses and values). Reading SHALL NOT modify the underlying file in any way. Views SHALL identify their elements precisely enough for the model to target them in later edit operations.

#### Scenario: A DOCX reads as a text view

- **WHEN** the model reads a DOCX file through the filesystem read path
- **THEN** the result presents the document's paragraphs and headings in order, and the file on disk is byte-for-byte unchanged

#### Scenario: An XLSX reads as a grid view

- **WHEN** the model reads an XLSX file through the filesystem read path
- **THEN** the result presents each sheet's populated cells with their addresses

### Requirement: In-place revision of office documents

A built-in tool SHALL apply targeted, element-level changes to a workspace DOCX, XLSX, or PPTX file — replacing, inserting, or deleting content at addressed elements and filling placeholders — and SHALL preserve every other aspect of the original file, including untouched text, styling, layout, and embedded objects. Rejected operations (unknown target, malformed operation) and processing failures SHALL be returned as readable tool results that do not end the turn.

#### Scenario: Replacing one paragraph preserves the rest

- **WHEN** the model replaces the text of one paragraph in a staged DOCX
- **THEN** the saved file shows the new text in that paragraph and every other paragraph's content and styling unchanged

#### Scenario: Filling placeholders in a template copy

- **WHEN** the model fills the placeholders of a staged DOCX template
- **THEN** each placeholder is replaced by the supplied value and the template's styling is unchanged

#### Scenario: Bad operation feeds back without ending the turn

- **WHEN** the model targets a paragraph that does not exist
- **THEN** the tool returns a readable not-found result and the turn continues

### Requirement: PDF structural operations

A built-in tool SHALL apply structural operations to PDF files in the workspace: merging documents, splitting or extracting page ranges, rotating pages, applying stamps or watermarks, and filling form fields. The tool SHALL NOT rewrite PDF text content; when asked to change a PDF's wording it SHALL explain in its result that text changes are made by regenerating the document.

#### Scenario: Merging PDFs

- **WHEN** the model merges two staged PDFs into one
- **THEN** the output PDF contains the pages of both inputs in order and the inputs are unchanged

#### Scenario: Text rewrite request is redirected

- **WHEN** the model asks the tool to change the wording of a PDF page
- **THEN** the result explains that PDF text is changed by regenerating the document and names the create_document path

### Requirement: Documents skill

The application SHALL ship a `documents` skill seeded into the global skills directory at startup when missing. Its description SHALL state that it applies whenever a request asks to read, revise, fill, or produce office documents or to manipulate PDFs. Its body SHALL teach the document playbook: view a document before editing it, apply targeted operations instead of regenerating whole files when fidelity matters, reference the user's presets when generating, and route PDF text changes through regeneration. The skill SHALL be discovered, gated, explicitly invocable as `$documents`, and shadowable by an agent-tier skill of the same name exactly like any global-tier skill.

#### Scenario: Skill is seeded on startup

- **WHEN** the app starts with no `documents` skill in the global skills directory
- **THEN** the skill is seeded and eligible to agents with skills enabled

#### Scenario: Agent tier shadows the playbook

- **WHEN** an agent's workspace skills directory holds its own `documents` skill
- **THEN** that agent's turns use the workspace version in place of the seeded one
