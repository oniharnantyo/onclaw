# workspace-reference-documents Specification

## Purpose
A persistent workspace library of reference documents: members upload service documentation (API manuals, specs, decks, sheets) once; the original file is stored as-is, deterministically indexed for full-text search, surfaced to agents through a compose-time manifest, and consulted on demand with locator-accurate citations — with no embedding stack and no LLM processing at ingest.

## Requirements

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
A reference document's original bytes SHALL be stored unmodified through the workspace's configured storage driver — the same per-workspace configuration the Storage settings pane controls and chat attachments honor (local by default, S3-compatible when configured) — never through an instance-level default that ignores workspace configuration. The driver that stored each blob SHALL be recorded on the document row, and every later read of that blob — capability-URL download, indexing and re-index conversion, runner mount materialization, and deletion — SHALL dispatch on the recorded backend so local and S3-backed documents coexist and remain byte-identical readable across driver switches. As a distinct blob kind from chat attachments, no transformed copy — converted markdown, extracted text, or normalized file — SHALL be stored as an agent-visible artifact. On re-upload (replace), the blob SHALL be written to the workspace's then-current driver, the recorded backend updated, and derived state rebuilt from the new bytes; on delete, the blob SHALL be removed from the backend recorded on the row, along with all derived state.

#### Scenario: Upload with workspace configured for S3
- **WHEN** a workspace's storage is configured for an S3-compatible bucket and a member uploads a reference document
- **THEN** the blob is stored in that bucket, the document row records the S3 backend, and the download via its capability URL returns the uploaded bytes byte-identical

#### Scenario: Mixed backends coexist across a driver switch
- **WHEN** a workspace has a document stored locally, then switches storage to S3 and uploads a second document
- **THEN** the first document still downloads byte-identical from local storage while the second is served from the bucket, with no migration of the first

#### Scenario: Agent mount reads from the recorded backend
- **WHEN** an agent run materializes its references mount for a document whose blob is stored on S3
- **THEN** the mounted file content matches the stored blob, fetched through the recorded backend

#### Scenario: Replace rebuilds derived state
- **WHEN** a member replaces `twilio-api.pdf` with a newer edition while the workspace is on S3
- **THEN** the new blob is stored under the workspace's current driver with its backend recorded, and the search index reflects the new content, with no leftover sections from the previous edition

#### Scenario: Delete removes from the recorded backend
- **WHEN** a member deletes a document whose recorded backend is S3
- **THEN** the blob is removed from the bucket and the document, its index, and its capability URL stop resolving
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

### Requirement: Manifest injection
For every agent run with document tools enabled, composition SHALL inject a reference-documents manifest: one compact entry per reference document visible to the run — name, one-line description, page (or equivalent) count, and the document's top-level (level-1) headings as its table of contents. The table of contents SHALL represent the whole document's chapter structure, not a prefix of its first sections. The manifest SHALL be budget-capped; entries exceeding the budget SHALL collapse their table of contents to the document name and description rather than being dropped. The manifest SHALL NOT be injected for runs whose agents lack document tools.

#### Scenario: Manifest names the library
- **WHEN** an agent with document tools starts a run in a workspace holding two reference documents
- **THEN** the agent's context includes both documents with descriptions and top-level TOCs

#### Scenario: Deep section is discoverable from the manifest
- **WHEN** a document's PostLogin payload section is its chapter 5 while chapters 1–4 precede it
- **THEN** the manifest's table of contents for that document includes the chapter-5 heading, so a request about PostLogin can be matched to the document before any tool call

#### Scenario: Budget collapse keeps discovery
- **WHEN** the workspace holds more documents than the manifest budget allows at full detail
- **THEN** later entries collapse to name and description, and no visible document disappears from the manifest entirely

### Requirement: Document search tool
The runtime SHALL expose a built-in tool `document.search` registered against the document tool family seam (catalog entry, permission key subject to the workspace tool gate, tool card, hook target — no new plumbing). Its input is a text query; its output is a bounded list of matching sections across all reference documents visible to the run, each hit naming the document, its heading, its locator when one exists, and a text snippet. Each hit SHALL carry a read hint naming the exact `document.read` invocation for that hit — the mount-relative document path and its locator — so the consuming model can read the section without constructing filesystem paths. Search SHALL be full-text over the section index without embeddings or model calls.

#### Scenario: Mixed-type hits for one query
- **WHEN** the library holds a PDF, a markdown note, and a CSV mentioning "sandbox rate limit" and the agent invokes `document.search` with that query
- **THEN** the result lists hits from all three documents, each with its own locator form (page, heading, none)

#### Scenario: Hit names its next read
- **WHEN** a search hit resolves to section "5.1 PostLogin" of `FDS_Sokratech_Spesifikasi_Input_Output.docx`
- **THEN** the hit carries a read hint equivalent to reading `references/FDS_Sokratech_Spesifikasi_Input_Output.docx` scoped to that section

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
In direct-chat and channel transcripts, `document.search` and `document.read` calls SHALL render as tool cards with verb, target, and latency. Agent citations of a reference document SHALL render as clickable chips carrying the document name and locator (page/slide/sheet/heading) that open the document preview in the right panel. The composer toolbar SHALL offer a documents affordance that opens the right panel's Documents listing — listing exactly the conversation-visible documents (direct chats resolving by agent attachments plus promoted, channels by channel attachments plus promoted) — replacing the former floating popover. The panel Documents listing SHALL offer preview on row click and an insert-as-mention action per row. Inserting a document mention SHALL add a pill to the composer text and attach a pointer note (document identity only, no content) to the turn. On surfaces without a right-panel host — workspace settings and the agent-config dialog — opening a document preview SHALL render the document source in a standalone modal rather than routing through the panel store.

#### Scenario: Citation chip opens the page
- **WHEN** an agent answers citing `twilio-api.pdf` page 31 and the user clicks the citation chip
- **THEN** the right panel opens the document preview positioned at that document

#### Scenario: Mention carries identity, not content
- **WHEN** a user inserts `integration-notes.md` as a mention from the panel Documents listing and sends "compare this with the official limits"
- **THEN** the turn carries a pointer note naming the document, and the agent uses its search and read tools to consult it

#### Scenario: Panel listing matches the run's visibility
- **WHEN** a user opens the Documents listing from the composer affordance in a channel where only one document is channel-attached
- **THEN** the listing shows that document plus promoted documents, and never a document the run could not read

#### Scenario: Preview works outside the chat route
- **WHEN** a user clicks a document preview control in workspace settings or the agent-config dialog
- **THEN** the document source renders in a modal on the current page, without navigating to chat
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

### Requirement: Document read path handling
The `document.read` tool SHALL treat different absolute spellings of the same file as the same file when checking its read roots — on macOS, the `/System/Volumes/Data/...` firmlink spelling and the plain `/...` spelling of one path SHALL both pass the root check. When a path is rejected as outside the allowed roots, the error SHALL name the accepted path forms — the `references/<document name>` mount form, the workspace-relative form, and the `/workspace/...` mount form — so a model can self-correct in one retry.

#### Scenario: Firmlink spelling accepted
- **WHEN** a document blob is reachable at `/Users/me/.onclaw/.../references/manual.pdf` and the tool is invoked with the `/System/Volumes/Data/Users/me/.onclaw/.../references/manual.pdf` spelling of the same file
- **THEN** the read succeeds, returning the same converted content

#### Scenario: Rejection teaches the correct form
- **WHEN** the tool is invoked with a path outside every allowed root
- **THEN** the error names `references/<document name>` and the workspace-relative form as accepted alternatives instead of a bare rejection

### Requirement: Document usage system skill
The runtime SHALL ship a system-tier skill named `document-read` that teaches agents the reference-document workflow: consulting the compose-time manifest before searching, discovering sections with `document.search`, reading with scoped `document.read` invocations (pages for PDF ranges, section for heading, slide, or sheet; a hit's own read hint when present), citing by document name plus locator linked to `references/<document name>`, delegating heavy multi-document research to the `agent` tool, and never locating or extracting documents through the shell. The skill SHALL be embedded with the binary and mirrored by the same boot-time sync as the other system skills, injected into every agent's execution per the system-tier rules, not disableable, and forkable to the workspace tier as the only customization path. The skill SHALL be content-accurate whether or not search hits carry read hints.

#### Scenario: Skill ships and syncs
- **WHEN** the server boots in a fresh instance
- **THEN** the system skills tree contains `document-read` beside the other embedded skills, mirrored from the binary

#### Scenario: Every agent sees the workflow
- **WHEN** any agent runs with document tools enabled
- **THEN** the skill's procedure is available to it as a system-tier skill, with no install or attach step

#### Scenario: Fork customizes, original locked
- **WHEN** a user forks the `document-read` system skill to the workspace tier
- **THEN** a workspace skill with source `fork` is created from the embedded content and the system skill itself remains locked and unchanged
