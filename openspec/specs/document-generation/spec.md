## Purpose

Let users define reusable presentation presets for the documents agents generate, manage them from the create_document tool's configuration, and make agents aware of them — the user controls how documents look, the agent only supplies content.

## Requirements

### Requirement: Document presets

A document preset SHALL be a named, format-scoped presentation configuration. PDF presets hold a rendering configuration: theme, page setup (size, orientation, margins), cover fields, footer style, watermark, and table-of-contents settings. DOCX presets come in two flavors: a template-document preset, which is a user-provided .docx skeleton whose styles and layout control the generated document's look while the agent supplies only content, and a config preset, whose parameters (fonts, heading style, page size, orientation, margins, footer) are applied over the application's built-in default template. PPTX presets are template-document presets only: a user-provided .pptx skeleton whose existing layouts and text placeholders are filled; new slide layouts SHALL NOT be created. The user SHALL create, edit, duplicate, and delete presets of every flavor. Presets SHALL survive restarts. Deleting the preset configured as a format's default SHALL reset that format's default selection to none. Custom CSS and signature blocks SHALL NOT be part of the preset surface.

A DOCX template skeleton's body SHALL be preserved in the output, and the generated content SHALL land at the skeleton's `{{content}}` marker paragraph when the skeleton carries one — replacing the marker at its position — and after the skeleton's body when it does not. Generated DOCX text SHALL carry inline bold and italic as character formatting on the affected runs, never as literal markdown markers; constructs the engines do not map (tables, code fences, images, horizontal rules) degrade to literal text as they do today, and the create_document guidance SHALL say so.

#### Scenario: Preset round-trips

- **WHEN** the user creates a DOCX template-document preset from an uploaded skeleton, then restarts the app
- **THEN** the preset is still listed with its template document in place

#### Scenario: Deleting the default preset resets the default

- **WHEN** the user deletes the preset configured as the PDF format's default
- **THEN** the PDF default selection is cleared and PDF tool calls without a preset name use the library defaults

#### Scenario: PPTX presets fill existing layouts only

- **WHEN** a document is generated from a PPTX template preset with more content than the skeleton's slides hold placeholders for
- **THEN** the generation fills the skeleton's placeholders and does not invent new slide layouts

#### Scenario: Content lands at the marker

- **WHEN** a document is generated from a DOCX skeleton whose body carries a `{{content}}` marker paragraph between its opening and closing sections
- **THEN** the output keeps the skeleton's body with the generated content at the marker's position and no marker paragraph remains

#### Scenario: Shell skeleton without a marker appends

- **WHEN** a document is generated from a DOCX skeleton that carries no `{{content}}` marker
- **THEN** the output keeps the skeleton's body with the generated content after it

#### Scenario: Inline emphasis is character formatting

- **WHEN** the generated content contains `**bold**` or `*italic*` spans
- **THEN** the DOCX output renders those spans as bold/italic runs and no literal asterisk markers remain in the document text

#### Scenario: PPTX placeholders carry plain text

- **WHEN** PPTX placeholder fill maps a content block containing inline markers
- **THEN** the placeholder text carries the words without the markdown markers

### Requirement: Preset management in the Tools pane

The create_document tool's Tools pane card SHALL offer a configuration action opening a dialog with one tab per supported format (PDF, DOCX, PPTX), where each tab lists that format's presets with create, edit, duplicate, and delete controls and hosts that format's default-preset selector. Creating a template-document preset SHALL accept uploading the skeleton file; the DOCX tab SHALL additionally offer the config-preset editor, which groups its fields by concern (fonts, heading style, page setup, footer) and saves atomically. The dialog SHALL render legibly in both themes.

#### Scenario: PDF tab manages presets

- **WHEN** the user opens the create_document configuration dialog
- **THEN** the PDF tab lists the existing PDF presets with their controls and the PDF default-preset selector

#### Scenario: DOCX tab manages template presets

- **WHEN** the user opens the create_document configuration dialog's DOCX tab
- **THEN** the tab lists the existing DOCX presets with their controls, the DOCX default-preset selector, and an upload control for adding a template-document preset

#### Scenario: Editor saves a grouped preset

- **WHEN** the user edits a PDF preset's page size, cover fields, and footer style and saves
- **THEN** the values persist and the next generation using that preset reflects them


### Requirement: Preset awareness in turns

When the create_document tool is offered to a turn and at least one preset exists, the turn's composed context SHALL list the available preset names, marking the default, so the model can reference presets without asking. With no presets, the composed context SHALL NOT include a preset listing.

When the create_document tool is offered to a turn, the model-facing guidance present that turn — the tool's own description and the operating contract — SHALL state the production route: document files (PDF, DOCX, PPTX) are produced through create_document, not by scripting generators under the shell or hand-assembling file formats, so routing holds even when no skill playbook is loaded. The guidance SHALL NOT restrict shell use for work that is not document-file production.

#### Scenario: Preset names listed when offered

- **WHEN** an agent with the tool offered receives a turn while presets "Client Report" and "Invoice" exist and "Client Report" is the default
- **THEN** the turn's context names both presets and marks "Client Report" as the default

#### Scenario: No presets, no listing

- **WHEN** no presets exist
- **THEN** the turn context contains no preset listing and tool calls without a preset name use the library defaults

#### Scenario: Routing stated without the playbook

- **WHEN** a turn offers create_document while no documents playbook skill body is loaded into the turn
- **THEN** the turn's model-facing guidance still states that document files are produced through create_document rather than shell-scripted generation
