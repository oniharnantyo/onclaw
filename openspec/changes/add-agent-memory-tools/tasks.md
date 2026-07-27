# Tasks

## 1. Store layer: `UpdateDocument` (interface + impl + mocks)

# Tasks

## 1. Store layer: `UpdateDocument` (interface + impl + mocks)

- [x] 1.1 Add `UpdateDocument(ctx, id int64, content string, vector []float32) error` to the `MemoryStore` interface in `internal/memory/store.go`
- [x] 1.2 Implement `UpdateDocument` in `internal/store/sqlite/memory.go`: `BeginTx` → `UPDATE memory_documents SET content=? WHERE id=?` → if `len(vector)>0`, `INSERT OR REPLACE INTO memory_embeddings (document_id, vector) VALUES (?,?)` → `Commit`; reuse `vectorToBlob`
- [x] 1.3 Add `UpdateDocument` to every `MemoryStore` mock so the tree compiles: `internal/agent/tools/memory_test.go`, `internal/agent/middlewares/memory_middleware_test.go`, `internal/memory/embedding_test.go`, `internal/memory/extract_test.go`
- [x] 1.4 TDD `UpdateDocument` in `internal/store/sqlite/memory_test.go`: happy path (content + embedding updated, FTS re-synced → `SearchArchive` finds new text, `id`+`created_at` preserved), not-found, vector-empty path
- [x] 1.5 `rtk go build ./...` passes

## 2. `memory_search` surfaces the document id

- [x] 2.1 In `internal/agent/tools/memory.go` `memorySearchTool`, change the result format to include the id: `"- [id N] <content> (relevance: 0.83)"`
- [x] 2.2 Update/add the `memory_search` test asserting the id appears in output (`internal/agent/tools/memory_test.go`)

## 3. New archive tools: `memory_remember` / `memory_update` / `memory_forget`

- [x] 3.1 Add `memoryRememberTool` (input `{content, kind?}`): `ScanContent` → dedup check (identical content for agent → "already remembered") → `Embedder.Embed` → `IndexDocument` (`Agent`, `Scope="global"`, `Kind="curated"` default, `Source="remember"`) → return `"Remembered (id N): …"`
- [x] 3.2 Add `memoryUpdateTool` (input `{id, content}`): `ScanContent` → re-embed → `UpdateDocument`; not-found → recoverable observation
- [x] 3.3 Add `memoryForgetTool` (input `{id}`): `GetDocument` (nil → recoverable "no memory with id N") → `DeleteDocument` → return `"Deleted (id N): …"`
- [x] 3.4 `Register` all three in `init()` alongside the existing memory tools; `Category() == "Memory"`
- [x] 3.5 TDD all three in `internal/agent/tools/memory_test.go`: remember insert + dedup-skip + ScanContent block; update success + not-found observation; forget success + not-found observation
- [x] 3.6 `rtk go build ./...` and `rtk go test ./internal/agent/tools/...` pass

## 4. Uniform tool management (memory always on)

- [x] 4.1 In `internal/cli/agent_session.go`, construct `memoryStore` (and sibling memory stores) **unconditionally**, not gated on `resolvedMem.Enabled`
- [x] 4.2 Remove the per-toggle filtering of `memory_search`/`session_search`/`kg_search` in `internal/agent/agent.go` `buildTools` so all memory tools are built by `tools.Builtin` and gated only by the standard enable/whitelist (update affected tests)
- [x] 4.3 Add the embedding-mode log line in `agent_session.go` after the embedder is built (`memory embeddings: <provider>/<model>` or `disabled (FTS-only)`)
- [x] 4.4 Insert the global embedding preference into the provider/model fallback chain in `agent_session.go`: agent override → global preference → bootstrap `memory.embedding_*` → chat provider
- [x] 4.5 `rtk go test ./internal/agent/... ./internal/cli/...` pass

## 5. Global embedding config — backend API

- [x] 5.1 Add `GetEmbeddingsConfig` / `SetEmbeddingsConfig` handlers in new `internal/api/handler/embeddings.go`, reading/writing `embedding_provider`, `embedding_model`, `embedding_api_base` in the `preferences` KV (model on existing `memory.go` handlers)
- [x] 5.2 Register `GET /api/config/embeddings` and `PUT /api/config/embeddings` (behind `requireAuth`) in `internal/api/routes.go`
- [x] 5.3 TDD the handlers in `internal/api/handler/embeddings_test.go` (GET/PUT round-trip, auth)

## 6. Global embedding config — web UI

- [x] 6.1 Add the **Embeddings** nav entry under the Configuration group in `web/src/App.tsx` (group at `:209`, alongside Providers) and a `/embeddings` route
- [x] 6.2 Create `web/src/pages/EmbeddingsPage.tsx` (model on `ProvidersPage.tsx`): provider select (from `GET /api/providers`), model override, optional API base; load via `GET /api/config/embeddings`, save via `PUT /api/config/embeddings`
- [x] 6.3 Verify the page loads saved values and saves persist (manual or component test)

## 7. Persona + docs

- [x] 7.1 Rewrite the Memory section of `internal/agent/templates/AGENTS.md` (lines 12-22): point "remember/update/forget this" at `memory_remember`/`memory_update`/`memory_forget`; remove the `memory/YYYY-MM-DD.md` convention
- [x] 7.2 Update `docs/memory.md`: retire the "documents are immutable" claim for Layer 2; add the agent write/update/delete triggers; note toggles now gate background subsystems only

## 8. Verification

- [x] 8.1 `rtk gofmt -w` on every touched Go file; `rtk gofmt -l` empty
- [x] 8.2 `make build` (static, `CGO_ENABLED=0`); `make vet`
- [x] 8.3 `rtk go test ./internal/memory/... ./internal/store/sqlite/... ./internal/agent/... ./internal/api/...` all pass, ≥70% coverage per package
- [x] 8.4 Manual agent run: `memory_remember` → `memory_search` (shows id) → `memory_update` by id → `memory_forget` by id; legacy "episodic" rows still found
- [x] 8.5 Confirm assembly log reports embedding mode; Embeddings page round-trips; agent inherits global default unless overridden
- [x] 8.6 Grep: no `memory/YYYY-MM-DD.md` references remain in `internal/agent/templates/`
