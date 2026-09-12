## MODIFIED Requirements

### Requirement: Three-lane format classification
Each attachment SHALL be classified into exactly one lane: the **inline lane** (image types, delivered to the model as native image blocks; text-like files at or under 200 KB, delivered as fenced text), the **drop lane** (documents — pdf, docx, xlsx, pptx — and text-like files over 200 KB up to 50 MB: code, config, logs — stored, mounted read-only for the run, and made readable to the agent's file tools and the `document.read` tool without entering model context wholesale), or the **reject lane** (legacy binary office formats doc/ppt, archives, executables, and unknown types, which SHALL be rejected at upload with guidance; legacy office formats suggest converting to a modern format or PDF). Rejection SHALL occur at upload time, not at turn time.

#### Scenario: Office format enters the drop lane
- **WHEN** a user uploads `report.docx`
- **THEN** the upload succeeds and the attachment is classified as drop-lane, mounted read-only and readable through the agent's file tools and the document read tool

#### Scenario: PDF enters the drop lane
- **WHEN** a user uploads a PDF at or under the size and page caps
- **THEN** the upload succeeds and the attachment is classified as drop-lane

#### Scenario: Office format rejected with guidance
- **WHEN** a user uploads a legacy binary office format (`.doc` or `.ppt`)
- **THEN** the upload is rejected with a message suggesting converting to a modern format (or PDF)

#### Scenario: Text-like file enters the drop lane
- **WHEN** a 4 MB SQL dump is uploaded
- **THEN** the upload succeeds and the attachment is classified as drop-lane, readable through the agent's file tools

#### Scenario: Small text file enters the inline lane
- **WHEN** a 40 KB `.yaml` config is uploaded
- **THEN** the upload succeeds and the attachment is classified as inline-lane text, delivered inside the turn message as a fenced text part

#### Scenario: Oversize PDF rejected
- **WHEN** a 40 MB PDF is uploaded
- **THEN** the upload is rejected with the PDF size cap stated
