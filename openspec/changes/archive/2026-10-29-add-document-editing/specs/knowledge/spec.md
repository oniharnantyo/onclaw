# Spec Delta

## MODIFIED Requirements

### Requirement: Document format and size gates

Every ingestion path — Knowledge menu upload, right-bar upload, and the agent's save tool — SHALL accept plain-text documents, PDF documents, and OOXML office documents (.docx, .xlsx, .pptx). PDFs and OOXML documents SHALL be at most 20 MiB and text documents at most 1 MiB; anything larger SHALL be rejected with the size reason. Any other kind (legacy word-processing formats, video, image, archives) SHALL be rejected with a one-line reason naming the supported kinds. Rejections MUST NOT create catalog rows or files.

#### Scenario: Office document is accepted at upload

- **WHEN** the user selects a `.docx` file in the Knowledge menu upload
- **THEN** the document is stored raw, listed with its description, and readable like any other document

#### Scenario: Unsupported kind is rejected at upload

- **WHEN** the user selects a `.mp4` video in the Knowledge menu upload
- **THEN** the upload is refused with a reason naming text, PDF, and OOXML documents as the supported kinds, and no document is created

#### Scenario: Oversize PDF is rejected

- **WHEN** the user uploads a PDF larger than 20 MiB
- **THEN** the upload is refused with the size reason and no document is created

#### Scenario: Oversize office document is rejected

- **WHEN** the user uploads an XLSX file larger than 20 MiB
- **THEN** the upload is refused with the size reason and no document is created

#### Scenario: Agent save enforces the same gates

- **WHEN** the agent's save tool is called with a video chat attachment
- **THEN** the tool returns a readable error result naming the supported kinds and the turn continues
