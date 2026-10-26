# Spec Delta

## MODIFIED Requirements

### Requirement: Workspace jail

The filesystem tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `delete_file`) SHALL operate only inside the agent's workspace directory, presented to the model as its workspace root, and inside the read-only knowledge corpus mount presented to the model as `/knowledge` (composed of the global documents and the session agent's scoped documents). Write operations (`write_file`, `edit_file`, `delete_file`) SHALL apply to the workspace only — the knowledge mount SHALL never be writable by filesystem tools. Paths resolving outside the jail SHALL return an error result, not end the turn.

#### Scenario: File edits stay in the workspace

- **WHEN** the model writes and then reads a file in the workspace
- **THEN** the operations succeed against the agent's workspace directory on disk

#### Scenario: Escape attempt is rejected

- **WHEN** the model requests a path outside the workspace
- **THEN** the tool returns an out-of-scope error result and the turn continues

#### Scenario: Knowledge corpus is readable through the tools

- **WHEN** the model lists, greps, or reads a path under `/knowledge`
- **THEN** the operation resolves against the global and session-agent knowledge roots on disk

#### Scenario: Knowledge mount is not writable

- **WHEN** the model attempts `write_file`, `edit_file`, or `delete_file` under `/knowledge`
- **THEN** the tool returns an error result, the corpus is unchanged, and the turn continues

### Requirement: Multimodal file reads

The `read_file` tool SHALL render supported binary files as structured multimodal parts instead of raw bytes: image files as image parts, and PDF files as rendered page parts. A PDF read SHALL support reading the whole document or a bounded page range, with the per-request page bound reported in the tool result when the range is clipped. The jail applies unchanged: only files inside the agent's workspace or the read-only `/knowledge` mount are readable, and files exceeding the configured size limits SHALL return a clear error result rather than ending the turn. Text files SHALL continue to read as text.

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

## ADDED Requirements

### Requirement: Save document tool in the catalog

The built-in tool catalog SHALL include a `save_document` tool (display name "Save document") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The tool SHALL accept either a chat attachment identified by its capability key or inline text content with a filename, MAY accept a short description recorded with the document, and SHALL save into the session agent's scope. It SHALL enforce the knowledge corpus format and size gates (text and PDF only); unsupported or oversize input returns a readable error result and the turn continues. A successful save registers the document in the catalog and returns its path under `/knowledge`. The tool is the only write path into the corpus: filesystem tools cannot create or modify knowledge documents.

#### Scenario: Agent saves a chat attachment

- **WHEN** the model calls `save_document` with the capability key of a PDF attached to the conversation
- **THEN** the document is registered in the agent's scope and the tool result reports its `/knowledge` path

#### Scenario: Agent saves composed text

- **WHEN** the model calls `save_document` with inline text content and a filename
- **THEN** the document is stored and registered like an uploaded text document

#### Scenario: Unsupported attachment is refused

- **WHEN** the model calls `save_document` with a video attachment's key
- **THEN** the tool returns a readable error naming the supported kinds and no document is created
