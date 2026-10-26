# Spec Delta

## Purpose

Give agents a durable document corpus they search with the tools they already have: users and agents save documents into per-agent or global scope, and every turn exposes the corpus read-only for terminal-style search — no embeddings, no vector index, no extraction pipeline.

## ADDED Requirements

### Requirement: Durable raw document corpus

The application SHALL maintain a knowledge corpus of documents stored raw (unmodified bytes, no extraction or companion files) under the application home: global documents available to every agent and agent-scoped documents available to one agent. Every document SHALL be cataloged with its filename, MIME type, size, scope, optional description, ingestion source, and creation time, and SHALL remain stored and listed across app restarts. Within a scope, document filenames SHALL be unique — saving or uploading a document whose filename already exists in the same scope SHALL be rejected with the reason, never silently overwritten.

#### Scenario: Document survives restart

- **WHEN** the user uploads a document and restarts the app
- **THEN** the document is still stored, listed, and readable through its scope

#### Scenario: Duplicate filename in a scope is rejected

- **WHEN** a document is saved into a scope that already has a document with the same filename
- **THEN** the save is rejected with a duplicate-name reason and the existing document is unchanged

### Requirement: Document format and size gates

Every ingestion path — Knowledge menu upload, right-bar upload, and the agent's save tool — SHALL accept only plain-text documents and PDF documents. PDFs SHALL be at most 20 MiB and text documents at most 1 MiB; anything larger SHALL be rejected with the size reason. Any other kind (word-processing, spreadsheet, presentation, video, image, archives) SHALL be rejected with a one-line reason naming the supported kinds. Rejections MUST NOT create catalog rows or files.

#### Scenario: Unsupported kind is rejected at upload

- **WHEN** the user selects a `.docx` file in the Knowledge menu upload
- **THEN** the upload is refused with a reason naming text and PDF as the supported kinds, and no document is created

#### Scenario: Oversize PDF is rejected

- **WHEN** the user uploads a PDF larger than 20 MiB
- **THEN** the upload is refused with the size reason and no document is created

#### Scenario: Agent save enforces the same gates

- **WHEN** the agent's save tool is called with a video chat attachment
- **THEN** the tool returns a readable error result naming the supported kinds and the turn continues

### Requirement: Document scoping and promotion

Each document SHALL have exactly one scope: global, or one agent. The user SHALL be able to promote an agent-scoped document to global and demote a global document to a chosen agent, from both the Knowledge menu and the chat right bar. A scope change SHALL move the document's stored file and update its catalog entry atomically — after the change the document is visible under its new scope only. Deleting an agent SHALL NOT silently destroy knowledge: the delete confirmation SHALL state that agent-scoped documents exist, and a confirmed delete SHALL remove them along with the agent.

#### Scenario: Promotion makes a document global

- **WHEN** the user promotes an agent-scoped document to global
- **THEN** every agent's subsequent turns can read the document and only the agent's prior scope loses it

#### Scenario: Demotion scopes a document to one agent

- **WHEN** the user demotes a global document to an agent
- **THEN** only that agent's subsequent turns can read the document

#### Scenario: Agent deletion states the knowledge consequence

- **WHEN** the user deletes an agent that has scoped knowledge documents
- **THEN** the confirmation names the scoped documents, and confirming removes them with the agent

### Requirement: Corpus exposure to turns

A turn SHALL expose the knowledge corpus to the model as a read-only `/knowledge` location presenting the global documents under `/knowledge/global/` and the session agent's scoped documents under `/knowledge/agent/`; documents scoped to other agents SHALL NOT be visible. The turn's operating guidance SHALL tell the model that saved documents live under `/knowledge`, that they are searched with the filesystem tools (list, grep, read) before answering questions the documents may cover, and to activate the knowledge-search skill when the request may concern saved documents. Delegated subagents SHALL receive the same corpus exposure as their parent turn.

#### Scenario: Agent sees own and global documents

- **WHEN** a turn lists `/knowledge` for an agent with its own scoped documents and global documents present
- **THEN** both sets are visible under `/knowledge/agent/` and `/knowledge/global/`, and no other agent's scoped documents appear

#### Scenario: Subagent inherits the corpus

- **WHEN** the agent delegates a task to a subagent
- **THEN** the subagent can read the same `/knowledge` contents as its parent

#### Scenario: Guidance names the corpus and the skill

- **WHEN** a turn runs with the corpus exposed
- **THEN** the model's instruction includes the `/knowledge` search guidance and the direction to activate the knowledge-search skill for document-related requests

### Requirement: Knowledge management menu

The application SHALL provide a dedicated top-level Knowledge menu outside chat for managing the corpus. It SHALL list every document with its name, description, scope, size, and creation time, and offer filtering by scope (all, global, per agent). It SHALL offer upload with an explicit scope choice (global or a specific agent), document preview (text rendered as text, PDF rendered as a document), description editing, deletion with confirmation, and promote/demote actions. The menu SHALL render correctly in both the light and dark themes.

#### Scenario: Upload with a scope choice

- **WHEN** the user uploads a document from the Knowledge menu and picks global as the scope
- **THEN** the document is listed as global and readable by every agent's subsequent turns

#### Scenario: Scope filtering

- **WHEN** the user filters the list to one agent
- **THEN** only that agent's scoped documents are listed

#### Scenario: Description edit persists

- **WHEN** the user edits a document's description and restarts the app
- **THEN** the edited description is still shown for the document

#### Scenario: Delete requires confirmation

- **WHEN** the user deletes a document
- **THEN** a confirmation is required and the document is removed from the list, catalog, and disk

#### Scenario: Both themes render

- **WHEN** the Knowledge menu is opened in the light and dark themes
- **THEN** the list, preview, upload, and management controls render correctly in both

### Requirement: Live catalog updates

When a document is saved, uploaded, promoted, demoted, or deleted while a surface listing knowledge documents is open (the chat right bar's knowledge panel or the Knowledge menu), that surface SHALL reflect the change without being reopened or reloaded.

#### Scenario: Mid-turn save appears live

- **WHEN** the agent saves a document with its save tool while the right bar's knowledge panel is open
- **THEN** the document appears in the panel's list without reopening it

### Requirement: Knowledge search skill

The application SHALL ship a `knowledge-search` skill seeded into the global skills directory at startup when missing. Its body SHALL teach corpus exploration — read the scope indexes first, grep for lexical anchors, read around hits, read PDF documents by page range, refine the search when the first query misses — and its description SHALL state that it applies whenever a request concerns the saved documents. The skill SHALL be discovered, gated, explicitly invocable as `$knowledge-search`, and shadowable by an agent-tier skill of the same name exactly like any global-tier skill. The corpus guidance SHALL direct the model to activate it when a request may concern saved documents.

#### Scenario: Skill is seeded on startup

- **WHEN** the app starts with no `knowledge-search` skill in the global skills directory
- **THEN** the skill is seeded and eligible to agents with skills enabled

#### Scenario: Guidance directs activation

- **WHEN** a turn exposes the corpus and the user asks a question their saved documents may answer
- **THEN** the turn's guidance directs the model to activate the knowledge-search skill

#### Scenario: Agent tier shadows the playbook

- **WHEN** an agent's workspace skills directory holds its own `knowledge-search` skill
- **THEN** that agent's turns use the workspace version in place of the seeded one

### Requirement: Corpus index

Each scope directory SHALL carry a store-maintained `INDEX.md` listing that scope's documents — filename, description, and size — regenerated on every catalog change. Index files SHALL be readable through the `/knowledge` mount and excluded from the document listings of the Knowledge menu and the right-bar knowledge panel.

#### Scenario: Index reflects an upload

- **WHEN** a document is uploaded into a scope
- **THEN** that scope's `INDEX.md` lists the document with its description and size

#### Scenario: Indexes stay out of management lists

- **WHEN** the Knowledge menu and the knowledge panel list documents
- **THEN** no `INDEX.md` entry appears among the listed documents
