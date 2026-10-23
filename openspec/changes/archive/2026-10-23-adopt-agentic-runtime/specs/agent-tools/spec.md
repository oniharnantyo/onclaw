# Spec Delta

## ADDED Requirements

### Requirement: Multimodal file reads

The `read_file` tool SHALL render supported binary files as structured multimodal parts instead of raw bytes: image files as image parts, and PDF files as rendered page parts. A PDF read SHALL support reading the whole document or a bounded page range, with the per-request page bound reported in the tool result when the range is clipped. The jail applies unchanged: only files inside the agent's workspace are readable, and files exceeding the configured size limits SHALL return a clear error result rather than ending the turn. Text files SHALL continue to read as text.

#### Scenario: PDF pages render as parts

- **WHEN** the model reads a PDF in the workspace with a page range
- **THEN** the result carries rendered parts for the requested pages only

#### Scenario: Image reads as a part

- **WHEN** the model reads an image file in the workspace
- **THEN** the result carries the image as a structured part rather than bytes

#### Scenario: Oversize file is rejected

- **WHEN** the model reads a file larger than the configured size limit
- **THEN** the tool returns a clear error result and the turn continues
