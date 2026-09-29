# Spec Delta

## Purpose

A persistent workspace library of reference documents: members upload service documentation (API manuals, specs, decks, sheets) once; the original file is stored as-is, deterministically indexed for full-text search, surfaced to agents through a compose-time manifest, and consulted on demand with locator-accurate citations — with no embedding stack and no LLM processing at ingest.

## ADDED Requirements

### Requirement: Reference document upload
The system SHALL provide an authenticated workspace-scoped upload endpoint for reference documents accepting multipart form-data, supporting pdf, docx, pptx, xlsx, md, txt, html, and csv. Uploads SHALL require workspace-member authentication; the document SHALL be recorded against the key's workspace and creator. The actual content type SHALL be determined from magic bytes, never the client-declared type, with per-type size caps mirroring the attachment contract (PDF 20 MB; office documents and text-like files per drop-lane caps). Legacy binary office formats (doc, ppt), archives, executables, and unknown types SHALL be rejected at upload with guidance. On success the endpoint SHALL return the document's id, name, detected type, byte size, capability URL, and index status.

#### Scenario: Member uploads a service manual
- **WHEN** a workspace member uploads a 12 MB PDF via multipart form-data
- **THEN** the response is `201` with `{id, name, mime, size, url, indexStatus}`, and the document appears in the workspace's reference documents

#### Scenario: Legacy format rejected with guidance
- **WHEN** a user uploads a `.doc` file as a reference document
- **THEN** the upload is rejected with a message suggesting conversion to a modern format or PDF

#### Scenario: Claimed type overridden by sniffing
- **WHEN** an executable is renamed to `manual.pdf` and uploaded
- **THEN** magic-byte detection identifies the real type and the upload is rejected as a disallowed type

### Requirement: As-is blob storage
A reference document's original bytes SHALL be stored unmodified through the storage port's driver abstraction (local and S3-compatible drivers, per-blob backend recorded, onclaw-proxied capability URLs) as a distinct blob kind from chat attachments. No transformed copy — converted markdown, extracted text, or normalized file — SHALL be stored as an agent-visible artifact. On re-upload (replace), the blob SHALL be swapped and derived state rebuilt from the new bytes; on delete, the blob and all derived state SHALL be removed.

#### Scenario: Download returns the original bytes
- **WHEN** a user downloads an uploaded reference document via its capability URL
- **THEN** the served bytes are byte-identical to the uploaded file

#### Scenario: Replace rebuilds derived state
- **WHEN** a member replaces `twilio-api.pdf` with a newer edition
- **THEN** the stored blob is the new file and the search index reflects the new content, with no leftover sections from the previous edition

### Requirement: Deterministic section index
At upload, the system SHALL convert the document to markdown in-process using the document converter registry (pdf, docx, pptx, xlsx, html, csv) or pass text formats through directly (md, txt), then split the result into sections with a format-agnostic sectioner keyed on markdown structure. Each section SHALL record the document, its heading or title, an ordinal, its body text, and a type-appropriate **locator**: page number for PDF, slide number for pptx, sheet name for xlsx, heading for docx/md/html, none for txt/csv. Section text SHALL be indexed for full-text search. PDF conversion SHALL emit page anchors so headings map to page locators. The extracted text SHALL NOT be retained outside the index; the index SHALL be rebuildable from the stored blob alone, and a rebuild SHALL produce equivalent sections. A scanned or image-only PDF with no extractable text layer SHALL surface a no-text-layer status at upload, and its index SHALL contain at most page-level sections.

#### Scenario: PDF sections carry page locators
- **WHEN** a PDF with a bookmark outline titled "Webhooks" starting at page 30 is uploaded
- **THEN** the index contains a section for "Webhooks" whose locator resolves to page 30

#### Scenario: xlsx sections are sheets
- **WHEN** a workbook with sheets `Users` and `RateLimits` is uploaded
- **THEN** each sheet becomes a section whose locator is the sheet name

#### Scenario: Unstructured text stays searchable
- **WHEN** a plain `.txt` integration note is uploaded
- **THEN** it is indexed as a single section with no locator and is findable by full-text search

#### Scenario: Scanned PDF warns at upload
- **WHEN** an image-only PDF with an empty text layer is uploaded
- **THEN** the upload succeeds with a no-text-layer status naming the limitation, and agents are not silently left with an empty index

### Requirement: Document search tool
The runtime SHALL expose a built-in tool `document.search` registered against the document tool family seam (catalog entry, permission key subject to the workspace tool gate, tool card, hook target — no new plumbing). Its input is a text query; its output is a bounded list of matching sections across all reference documents visible to the run, each hit naming the document, its heading, its locator when one exists, and a text snippet. Search SHALL be full-text over the section index without embeddings or model calls.

#### Scenario: Mixed-type hits for one query
- **WHEN** the library holds a PDF, a markdown note, and a CSV mentioning "sandbox rate limit" and the agent invokes `document.search` with that query
- **THEN** the result lists hits from all three documents, each with its own locator form (page, heading, none)

#### Scenario: Tool renders a card and respects the gate
- **WHEN** an agent turn includes a `document.search` call
- **THEN** the transcript renders a tool card with the query and latency, and agents without the document tools enabled cannot invoke it

### Requirement: Manifest injection
For every agent run with document tools enabled, composition SHALL inject a reference-documents manifest: one compact entry per reference document visible to the run — name, one-line description, page (or equivalent) count, and top-level table of contents. The manifest SHALL be budget-capped; entries exceeding the budget SHALL collapse their table of contents to the document name and description rather than being dropped. The manifest SHALL NOT be injected for runs whose agents lack document tools.

#### Scenario: Manifest names the library
- **WHEN** an agent with document tools starts a run in a workspace holding two reference documents
- **THEN** the agent's context includes both documents with descriptions and top-level TOCs

#### Scenario: Budget collapse keeps discovery
- **WHEN** the workspace holds more documents than the manifest budget allows at full detail
- **THEN** later entries collapse to name and description, and no visible document disappears from the manifest entirely

### Requirement: Tiered visibility
A reference document SHALL be visible to a run if and only if it is promoted to the workspace, OR the run's agent is in the document's attached agents, OR the run is a channel session whose channel is in the document's attached channels. New documents SHALL default to attached scope: the uploader selects agents and/or channels they can configure. Promotion to all agents in the workspace and demotion back to attached scope SHALL be admin-only operations gated by the permission catalog. Visibility SHALL be enforced at composition (manifest) AND at query time (`document.search` and scoped reads), never by prompt hiding alone. Scheduled runs SHALL resolve visibility by agent bindings only; a channel delivery target SHALL NOT widen visibility.

#### Scenario: Channel-tied document is chat-scoped
- **WHEN** a runbook is attached only to `#incidents` and the same agent searches from a direct chat
- **THEN** the runbook is absent from the manifest and from search results in that chat, and present in runs inside `#incidents`

#### Scenario: Promotion requires admin
- **WHEN** a non-admin member attempts to promote a document to all agents
- **THEN** the operation is rejected with a permission error, and no such toggle is offered in their UI

#### Scenario: Scheduler does not inherit channel scope
- **WHEN** a scheduled run of Atlas delivers to `#incidents`, and a document is attached only to `#incidents`
- **THEN** that document is not visible to the scheduled run

### Requirement: Management surfaces
The workspace settings SHALL provide a Documents pane as the single edit surface: upload, edit name and description, change attachment (agents/channels), promote/demote (admin), replace, and delete, with per-document index status and type. The agent config modal SHALL show a read-only reference-documents section listing promoted documents and documents attached to that agent, each with a scope badge and preview link. Channel settings SHALL show a read-only section listing documents attached to that channel plus promoted documents. All three surfaces SHALL offer preview via the document's capability URL in the right panel, and SHALL present empty states per the design contract.

#### Scenario: Agent view mirrors reality
- **WHEN** an admin promotes a document after it was attached to Atlas
- **THEN** Atlas's config modal lists the document with an "all agents" badge, and no edit control exists on the agent side

#### Scenario: Member sees own scope only
- **WHEN** a non-admin member opens the Documents pane
- **THEN** they see documents they uploaded plus documents attached to agents or channels they administer, without promote controls

### Requirement: Chat surfaces
In direct-chat and channel transcripts, `document.search` and `document.read` calls SHALL render as tool cards with verb, target, and latency. Agent citations of a reference document SHALL render as clickable chips carrying the document name and locator (page/slide/sheet/heading) that open the document preview in the right panel. The composer SHALL offer a documents popover listing exactly the conversation-visible documents — direct chats resolving by agent attachments plus promoted, channels by channel attachments plus promoted — with preview and insert-as-mention actions. Inserting a document mention SHALL add a pill to the composer text and attach a pointer note (document identity only, no content) to the turn. The right panel SHALL provide a Documents source listing conversation-visible documents with in-panel preview.

#### Scenario: Citation chip opens the page
- **WHEN** an agent answers citing `twilio-api.pdf` page 31 and the user clicks the citation chip
- **THEN** the right panel opens the document preview positioned at that document

#### Scenario: Mention carries identity, not content
- **WHEN** a user inserts `integration-notes.md` as a mention and sends "compare this with the official limits"
- **THEN** the turn carries a pointer note naming the document, and the agent uses its search and read tools to consult it

#### Scenario: Popover matches the run's visibility
- **WHEN** a user opens the documents popover in a channel where only one document is channel-attached
- **THEN** the popover lists that document plus promoted documents, and never a document the run could not read

### Requirement: Reference document tenancy
A reference document SHALL be reachable only within its workspace: resolving a document id or path from another workspace SHALL fail not-found, indistinguishable from an unknown document. Attachment references SHALL be cleaned up when an attached agent or channel is deleted.

#### Scenario: Foreign document unresolvable
- **WHEN** a run or API call references a reference-document id belonging to another workspace
- **THEN** resolution fails not-found with the same shape as an unknown id

### Requirement: Subagent inheritance
A subagent session spawned by an agent with document tools SHALL resolve `document.search` in its tool registry and receive the reference-documents manifest for its parent's visibility scope. Heavy document research SHALL remain the agent's own routing decision via the existing subagent tool; no document-specific routing or dedicated child-agent type is introduced.

#### Scenario: Spawned session can consult the library
- **WHEN** Atlas spawns a subagent to compare webhook signing across reference documents
- **THEN** the subagent session lists the visible documents in its manifest and invokes `document.search` successfully
