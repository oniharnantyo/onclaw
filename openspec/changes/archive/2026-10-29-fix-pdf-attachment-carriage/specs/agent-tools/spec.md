# Spec Delta

## MODIFIED Requirements

### Requirement: Multimodal file reads

The `read_file` tool SHALL render supported binary files as structured multimodal parts instead of raw bytes: image files as image parts, and PDF files as rendered page parts. A PDF read SHALL support reading the whole document or a bounded page range, with the per-request page bound reported in the tool result when the range is clipped. The jail applies unchanged: only files inside the agent's workspace or the read-only `/knowledge` mount are readable, and files exceeding the configured size limits SHALL return a clear error result rather than ending the turn. Text files SHALL continue to read as text.

When the turn's provider connector cannot carry a PDF file part in tool results, a PDF read SHALL degrade to a readable text result carrying the document's extracted text instead of a file part, so the read never fails the turn; the result SHALL say the bytes were not attached when they were withheld for this reason. When the document yields no extractable text, the read SHALL return a clear error result saying so, and the turn continues.

#### Scenario: PDF pages render as parts

- **WHEN** the model reads a PDF in the workspace with a page range
- **THEN** the result carries rendered parts for the requested pages only

#### Scenario: Image reads as a part

- **WHEN** the model reads an image file in the workspace
- **THEN** the result carries the image as a structured part rather than bytes

#### Scenario: Oversize file is rejected

- **WHEN** the model reads a file larger than the configured size limit
- **THEN** the tool returns a clear error result and the turn continues

#### Scenario: Corpus PDF renders as parts

- **WHEN** the model reads a PDF under `/knowledge` with a page range
- **THEN** the result carries rendered parts for the requested pages, read from the knowledge roots

#### Scenario: PDF read on a connector without file-part carriage

- **WHEN** the model reads a PDF while the turn's provider connector cannot carry PDF file parts in tool results
- **THEN** the result carries the extracted text as a readable text result noting the bytes were withheld, and the turn continues

#### Scenario: PDF with no extractable text on such a connector

- **WHEN** the model reads a scanned PDF that yields no extractable text while the connector cannot carry PDF file parts
- **THEN** the tool returns a clear error result saying no text could be extracted, and the turn continues
