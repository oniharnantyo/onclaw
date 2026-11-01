## MODIFIED Requirements

### Requirement: Reading office documents

The jailed filesystem read path SHALL present office documents as model-readable views: DOCX and PPTX as structured text views (paragraphs, headings, slide text in document order) and XLSX as a cell-grid view (sheet names with cell addresses and values). Reading SHALL NOT modify the underlying file in any way. Views SHALL identify their elements precisely enough for the model to target them in later edit operations.

The read tool's model-facing description SHALL state that office documents (DOCX, XLSX, PPTX) present as structured views, so reads of those formats route through the read path rather than shell unpacking, independent of whether a documents playbook skill body is loaded that turn.

#### Scenario: A DOCX reads as a text view

- **WHEN** the model reads a DOCX file through the filesystem read path
- **THEN** the result presents the document's paragraphs and headings in order, and the file on disk is byte-for-byte unchanged

#### Scenario: An XLSX reads as a grid view

- **WHEN** the model reads an XLSX file through the filesystem read path
- **THEN** the result presents each sheet's populated cells with their addresses

#### Scenario: Reads steered without the playbook

- **WHEN** a turn offers the read tool while no documents playbook skill body is loaded into the turn
- **THEN** the read tool's description states that office documents present as structured views and should be read through it rather than unpacked with shell commands
