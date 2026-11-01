## MODIFIED Requirements

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
