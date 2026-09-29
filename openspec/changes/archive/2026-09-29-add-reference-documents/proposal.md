# Proposal

## Why

Workspaces integrate with external services whose documentation — PDF manuals, API specs, slide decks, endpoint sheets — is uploaded once and then invisible: chat attachments live and die with a single run, so every new conversation starts from nothing and the agent re-asks for the same files. Teams need a persistent reference library the agent discovers on its own and consults with page-accurate citations, without adding an embedding stack or per-upload LLM processing.

## What Changes

- New workspace-level **reference documents library**: members upload `pdf`, `docx`, `pptx`, `xlsx`, `md`, `txt`, `html`, `csv`; every file is stored **as-is** (original bytes, capability-URL served) through the existing attachment storage port — no transformed second artifact.
- **Deterministic indexing at upload** (zero LLM calls, zero embeddings): the existing pure-Go document converters produce markdown in-memory; a format-agnostic sectioner splits it into sections with per-type **locators** (PDF page, pptx slide, xlsx sheet, docx/md/html heading, none for txt/csv); sections land in a Postgres FTS table (`tsvector` + GIN). The extracted text is discarded — the blob is the single source of truth and the index is rebuildable from it.
- New built-in tool **`document.search`** registered against the `document.*` family seam: FTS over sections, returns mixed-type hits as `(doc, heading, locator, snippet)`; hits filtered by the run's document visibility.
- **`document.read` gains scoped reads** (delta to `workspace-document-tools`): optional `pages` (PDF) and `section` (heading/slide/sheet title) parameters; the read jail extends with a read-only `references/` mount.
- **Manifest injection at compose**: a compact, budget-capped list of the run's visible reference documents (name, one-line description, page count, top-level TOC) injected like skills metadata, so the agent knows the library exists.
- **Tiered visibility**: documents are attached to specific agents and/or channels by default (visible to those agents' runs, and to any agent while running in those channels); admins can **promote** a document to all agents in the workspace and demote it back. The visibility predicate is enforced at compose AND at `document.search` query time.
- **Surfaces**: Settings → Documents pane (management, the single edit surface); read-only document sections in the agent config modal and channel settings; in-chat tool cards, clickable citation chips (existing markdown→right-panel link behavior), a composer documents popover with mention-as-pointer insertion, and a Documents source in the right panel.
- Subagent story needs no new lane: heavy research delegates via the existing subagent tool by the agent's own judgment; one test pins that spawned sessions inherit the manifest and document tools.

## Capabilities

### New Capabilities
- `workspace-reference-documents`: persistent workspace library of as-is uploaded reference documents with deterministic FTS indexing, locator-accurate search and scoped reads, compose-time manifest injection, tiered agent/channel visibility with admin promotion, and the settings/agent/channel/chat surfaces.

### Modified Capabilities
- `workspace-document-tools`: `document.read` input contract extends with optional `pages` and `section` scope parameters (PDF page anchors emitted by the PDF converter); the read-path jail gains the run's read-only references mount alongside drop-lane mounts and the agent workspace tree.

## Impact

- **Backend**: `internal/domain` (entity, validation, visibility predicate, permission entry), new migration pair (`reference_documents`, `document_sections` + attachment join tables), `internal/store` + `internal/store/postgres` (registry, sections, FTS queries, scoped lists), `internal/agents` (compose injection, `document.search` tool registration + implementation, `document.read` params + jail mount, PDF converter page anchors), `internal/server/handlers` + router (upload/list/patch/delete/promote/demote endpoints), `internal/attachments`-adjacent storage wiring (new blob kind, capability-URL serving reuse).
- **Frontend**: `web/src` — workspace settings Documents pane; agent config modal documents section; channel settings documents section; chat composer documents popover + mention pointer pill; right-panel Documents source; `document.search` tool card; `web/src/lib` documents API client.
- **Prompting**: base-prompt lines for reference-document usage and citations; subagent delegation hint.
- **Verification surface**: `scripts/smoke.sh` new sections (upload → index → search → scoped read → manifest presence → visibility filtering); Go unit + Postgres integration tests; web tests.
- **Non-goals**: vector/embedding retrieval (recorded as upgrade path), per-document LLM gisting, chat-drop upload flow (stub only), legacy `.doc`/`.ppt` acceptance (still rejected with conversion guidance, mirroring attachments).
