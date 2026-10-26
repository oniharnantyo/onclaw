# Spec Delta

## Purpose

Let users define reusable presentation presets for the documents agents generate, manage them from the create_document tool's configuration, and make agents aware of them — the user controls how documents look, the agent only supplies content.

## ADDED Requirements

### Requirement: Document presets

A document preset SHALL be a named, format-scoped presentation configuration: in v1 every preset is scoped to the PDF format and holds theme, page setup (size, orientation, margins), cover fields, footer style, watermark, and table-of-contents settings. The user SHALL create, edit, duplicate, and delete presets. Presets SHALL survive restarts. Deleting the preset configured as default SHALL reset the default selection to none. Custom CSS and signature blocks SHALL NOT be part of the v1 preset surface.

#### Scenario: Preset round-trips

- **WHEN** the user creates preset "Client Report" with a theme, A4 portrait, a cover, and a page-number footer, then restarts the app
- **THEN** the preset is still listed with those values

#### Scenario: Deleting the default preset resets the default

- **WHEN** the user deletes the preset configured as default
- **THEN** the default selection is cleared and tool calls without a preset name use the library defaults

### Requirement: Preset management in the Tools pane

The create_document tool's Tools pane card SHALL offer a configuration action opening a dialog with format tabs — PDF is the only tab in v1 — where each tab lists that format's presets with create, edit, duplicate, and delete controls, and hosts the format's default-preset selector. The preset editor SHALL group its fields by concern (theme, page setup, cover, footer, watermark, table of contents) and SHALL save atomically. The dialog SHALL render legibly in both themes.

#### Scenario: PDF tab manages presets

- **WHEN** the user opens the create_document configuration dialog
- **THEN** the PDF tab lists the existing presets with their controls and the default-preset selector

#### Scenario: Editor saves a grouped preset

- **WHEN** the user edits a preset's page size, cover fields, and footer style and saves
- **THEN** the values persist and the next generation using that preset reflects them

### Requirement: Preset awareness in turns

When the create_document tool is offered to a turn and at least one preset exists, the turn's composed context SHALL list the available preset names, marking the default, so the model can reference presets without asking. With no presets, the composed context SHALL NOT include a preset listing.

#### Scenario: Preset names listed when offered

- **WHEN** an agent with the tool offered receives a turn while presets "Client Report" and "Invoice" exist and "Client Report" is the default
- **THEN** the turn's context names both presets and marks "Client Report" as the default

#### Scenario: No presets, no listing

- **WHEN** no presets exist
- **THEN** the turn context contains no preset listing and tool calls without a preset name use the library defaults
