# Spec Delta

## MODIFIED Requirements

### Requirement: Document presets

A document preset SHALL be a named, format-scoped presentation configuration. PDF presets hold a rendering configuration: theme, page setup (size, orientation, margins), cover fields, footer style, watermark, and table-of-contents settings. DOCX presets come in two flavors: a template-document preset, which is a user-provided .docx skeleton whose styles and layout control the generated document's look while the agent supplies only content, and a config preset, whose parameters (fonts, heading style, page size, orientation, margins, footer) are applied over the application's built-in default template. PPTX presets are template-document presets only: a user-provided .pptx skeleton whose existing layouts and text placeholders are filled; new slide layouts SHALL NOT be created. The user SHALL create, edit, duplicate, and delete presets of every flavor. Presets SHALL survive restarts. Deleting the preset configured as a format's default SHALL reset that format's default selection to none. Custom CSS and signature blocks SHALL NOT be part of the preset surface.

#### Scenario: Preset round-trips

- **WHEN** the user creates a DOCX template-document preset from an uploaded skeleton, then restarts the app
- **THEN** the preset is still listed with its template document in place

#### Scenario: Deleting the default preset resets the default

- **WHEN** the user deletes the preset configured as the PDF format's default
- **THEN** the PDF default selection is cleared and PDF tool calls without a preset name use the library defaults

#### Scenario: PPTX presets fill existing layouts only

- **WHEN** a document is generated from a PPTX template preset with more content than the skeleton's slides hold placeholders for
- **THEN** the generation fills the skeleton's placeholders and does not invent new slide layouts

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
