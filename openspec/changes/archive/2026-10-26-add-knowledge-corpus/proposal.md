# Proposal

## Why

Chat attachments are ephemeral: they ride one turn's input and can never be found again, so agents re-explain the same documents in every session. Users need a durable, scoped document corpus their agents search on demand — and per the Direct Corpus Interaction approach (arXiv 2605.05242), buddies needs no retrieval stack to offer it: the jailed filesystem tools (ls, glob, grep, read_file, execute) already are the retrieval engine. What is missing is the corpus itself, ingestion paths, scoping, and management surfaces.

## What Changes

- Add a durable knowledge corpus on disk under `~/.buddies/knowledge/` with two scopes: `global/` (all agents) and `agents/<slug>/` (one agent), cataloged in a new `documents` table (migration 000014).
- Expose the corpus to agent turns as a second, **read-only** virtual mount `/knowledge` in the filesystem jail (alongside `/workspace`), composed per turn from the global tree plus the active agent's tree; subagents inherit it.
- Add a `save_document` catalog tool — the sole write path into the corpus — letting the agent save a chat attachment or inline text as a knowledge document during a turn.
- Add user upload in the chat right bar (defaults to the active agent's scope) and a new dedicated top-level **Knowledge** menu for managing documents outside chat: list, preview, upload, describe, delete, and promote/demote scope.
- Ship a seeded `knowledge-search` skill in the global skills tier (repaired at startup if missing) teaching corpus exploration; the corpus guidance directs the model to activate it whenever a request may concern saved documents, and `$knowledge-search` invokes it explicitly. The store maintains a per-scope `INDEX.md` (filename, description, size) so descriptions — the primary discovery handle for PDFs under raw storage — are visible to the model's tools.
- Accepted formats are `text/*` and `application/pdf` only (PDFs capped at 20 MiB, the multimodal-read ceiling); everything else is rejected with the reason shown. No embeddings, vector store, chunking, or extraction pipeline — documents stay raw and the agent searches them with the terminal tools it already has.

## Capabilities

### New Capabilities

- `knowledge`: Durable per-agent and global document corpus — ingestion (agent tool, right-bar upload, Knowledge menu upload), format and size gates, scoping with promote/demote, the read-only `/knowledge` jail exposure with directed skill activation, the store-maintained scope indexes, the seeded knowledge-search skill, the Knowledge management menu, and the document catalog service.

### Modified Capabilities

- `agent-tools`: The filesystem-tools requirement gains the read-only `/knowledge` mount (resolved from the knowledge roots, never writable, presented to the model as the knowledge corpus); a new requirement adds the `save_document` tool to the catalog, gated like every other tool.
- `data-store`: The home-directory-layout requirement gains the `~/.buddies/knowledge/` tree; a new requirement covers the `documents` catalog table (migration 000014) and its scope/agent linkage.
- `chat-panels`: The preview-sources requirement gains knowledge documents as a preview source read through a knowledge-scoped reader; a new requirement covers the right bar's knowledge panel — the active agent's documents plus global documents, with upload (defaulting to the agent's scope) and promote affordances.

## Impact

- **Go backend**: new `internal/knowledge` store (disk truth + catalog + index regeneration + global-tier skill seeding), `internal/services/knowledge.go` bound service (bindings → transport → client, `knowledge:changed` event), migration 000014, jail extension in `internal/agents/backend/fs_jailed_backend.go` (second read-only mount), `save_document` in `internal/agents/builtin_tools.go`, per-turn capability wiring in `internal/agents/runner.go` / `gate.go`, agent-delete handling for agent-scoped documents, contract/mount guidance.
- **Frontend**: new `features/knowledge` menu view (nav union + rail entry in `App.tsx`/`nav.ts`), right-bar knowledge panel (new `PanelTab` kind + renderer in `panelRenderers.tsx`), upload flow reusing the attachments base64 pattern, document preview, KnowledgeService client additions.
- **No new external dependencies** — no embedding model, no vector store, no extraction library.
- Out of scope: OOXML formats (docx/pptx/xlsx — rejected at upload for now; stdlib zip+XML decode-at-read remains the documented future path), images and video in knowledge lanes, semantic/vector retrieval, cross-device sync.
