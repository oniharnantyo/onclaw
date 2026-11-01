## MODIFIED Requirements

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
