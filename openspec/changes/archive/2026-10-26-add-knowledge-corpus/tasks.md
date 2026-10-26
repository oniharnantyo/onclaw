# Tasks

## 1. Knowledge store, catalog, layout

- [x] 1.1 Migration `internal/store/sqlite/migrations/000014_knowledge.sql`: `documents` table (id, filename, mime, size, scope `global|agent`, agent linkage with `ON DELETE CASCADE`, agent_slug for pathing, description, source `user|agent`, created_at; per-scope filename uniqueness) + bump the version pin in `internal/store/sqlite/db_test.go`
- [x] 1.2 Store layer: `internal/store/knowledge.go` interface + `internal/store/sqlite/knowledge.go` impl + fake registry entry, mirroring the attachments store seam
- [x] 1.3 `internal/knowledge` package: disk-truth store over `~/.buddies/knowledge/{global,agents/<slug>}` — single ingestion gate (text/\* ≤1 MiB, application/pdf ≤20 MiB, other kinds rejected with a named-kinds reason), per-scope filename uniqueness, save/list/get/content/set-scope (atomic move + compensating rollback)/set-description/delete, agent-scope removal for agent deletion, per-scope `INDEX.md` regeneration on every mutation (excluded from catalog and listings)
- [x] 1.4 Home layout: `Knowledge` root in `internal/config` (types, resolve, ensure) + config tests
- [x] 1.5 Seeded `knowledge-search` skill written into the global skills dir at startup when missing (repair-if-deleted; log-not-fatal like `SeedCanonicalModels`), body = DCI playbook, description states its trigger

## 2. Jail mount

- [x] 2.1 Second read-only virtual mount in `internal/agents/backend/fs_jailed_backend.go`: `/knowledge/global/...` and `/knowledge/agent/...` resolve against the two knowledge roots; `ls`/`glob`/`grep`/`read_file`/multimodal PDF reads work through it; write resolution untouched so `write_file`/`edit_file`/`delete_file` under `/knowledge` are rejected
- [x] 2.2 Tests: knowledge reads resolve; `/knowledge` write attempts rejected with corpus unchanged; escape attempts (`..`, symlinked roots) rejected; missing agent dir tolerated

## 3. Agents integration

- [x] 3.1 `save_document` catalog tool (`internal/agents/builtin_tools.go` + handler file): attachment-key or inline-text + filename input, optional description, saves into the session agent's scope, enforces the ingestion gates, returns the `/knowledge` path; withheld (not failed) when no knowledge store
- [x] 3.2 Turn wiring: knowledge roots composed into the fs capability at all three sites (fresh turns `runner.go`, resumed turns `resume.go`, subagent children `subagents.go` via `subagents.Config`), only when the agent is workspace-rootable; `compose.FilesystemCapability` carries the mount; subagents inherit the corpus
- [x] 3.3 Corpus guidance at the `composeSystem` seam: appended after the operating contract and before the compaction summary, only when the mount is exposed; names `/knowledge`, the scope subdirs, search-via-filesystem-tools, and directs activating the `knowledge-search` skill for document-related requests; empty guidance keeps `composeSystem` output byte-identical (pinned contract tests stay green)
- [x] 3.4 Agent deletion: `AgentsService.Delete` removes the agent's knowledge files best-effort after the row delete (rows cascade via FK); tests cover rows + files removal while global/other-agent documents remain

## 4. Bound service + wiring

- [x] 4.1 `internal/services/knowledge.go`: List / Upload (base64) / Delete / SetScope / SetDescription / Content bound methods over the store; `knowledge:changed` emitted on every mutation (event const + payload in `events.go`, `RegisterEvent` in desktop `main.go`, services bound in both `main.go` and `server_host.go`, `ServiceSet` entry in `internal/cli/app.go`)

## 5. Frontend — Knowledge menu

- [x] 5.1 Service client seam: `transport.ts` Knowledge block over the generated bindings, `client.ts` interface + impl + `onKnowledgeChanged` wrapper, `events.ts` constant
- [x] 5.2 `features/knowledge` top-level view + 5-step nav wiring (`nav.ts` Menu union, `App.tsx` menus/barMenus/route/titlebar): list (name, description, scope, size, created), scope filter (all/global/per-agent), upload with explicit scope choice, preview (text as text with markdown toggle for `.md`/`.markdown`, PDF as document), description editing, delete with confirmation, promote/demote
- [x] 5.3 Live updates: menu re-lists on `knowledge:changed` (coalesced refetch pattern); renders in light and dark themes
- [x] 5.4 Agent-delete confirmation names agent-scoped knowledge documents when any exist (`DeleteAgentModal`)

## 6. Frontend — right-bar knowledge panel + previews

- [x] 6.1 Right-bar knowledge panel opened from the identity view: lists the session agent's documents + global documents with scope markers; upload defaulting to the agent's scope with global as explicit choice; promote action; reflects mid-turn `save_document` saves live
- [x] 6.2 Knowledge preview tab: new `PanelTab` kind + renderer; text rendered as text (markdown toggle for `.md`/`.markdown`), PDF rendered as a document; read through the knowledge-scoped service (no filesystem reach beyond the knowledge roots)

## 7. Verification

- [x] 7.1 `gofmt -w .` clean, `go vet ./...` clean, `go test ./...` green
- [x] 7.2 `task build` green with bindings regenerated; frontend `vp check src` and scoped vitest green
- [x] 7.3 Every spec-delta scenario maps to a test or structural check; `openspec validate add-knowledge-corpus --strict` passes
