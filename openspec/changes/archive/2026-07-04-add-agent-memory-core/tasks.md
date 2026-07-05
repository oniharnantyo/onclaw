## 1. Store layer (contract/types/impl)

- [x] 1.1 `internal/memory/types.go` — DTOs: `MemoryDocument` (id, agent, scope, kind, content, source, createdAt), `MemoryHit`, `CoreEntry`, `ArchiveQuery`
- [x] 1.2 `internal/memory/store.go` — `MemoryStore` interface (`IndexDocument`, `SearchArchive`, `GetDocument`, `DeleteDocument`) and `CoreStore` (`ReadCore`, `WriteCore(add/replace/remove)`) — interfaces only, no impl
- [x] 1.3 `internal/store/sqlite/memory.go` — `memory_documents` (+ FTS5 virtual table), `memory_embeddings(vector BLOB)`, `embedding_cache(content_hash→BLOB)`; `sqliteMemoryStore` impl
- [x] 1.4 `internal/store/sqlite/memory_test.go` — black-box `<pkg>_test`: index/search/delete; FTS match; vector-cosine match; hybrid ordering; cache hit reuses embedding

## 2. Search + embeddings

- [x] 2.1 `internal/memory/search.go` — hybrid rank `0.3·FTS_norm + 0.7·cosine_norm`; FTS5 candidate prefilter; per-query embed + Go-cosine over BLOBs; per-scope boost; dedup
- [x] 2.2 `internal/memory/embedding.go` — wraps eino-ext `embedding.Embedder` (openai/gemini/ollama providers); `embedding_cache` read/write keyed by content hash; graceful FTS-only fallback when provider is nil or unreachable
- [x] 2.3 `internal/memory/search_test.go` — weight normalization; prefilter bounds cosine set; cache hit; FTS-only fallback when embedder off

## 3. Curated core (`MEMORY.md`)

- [x] 3.1 `internal/memory/core.go` — bounded read/write of `MEMORY.md`; **error-on-overflow** at the char cap (no silent truncation); `add`/`replace`/`remove` with substring matching (unique-match rule like `edit_file`)
- [x] 3.2 `internal/memory/security.go` + `_test.go` — scan writes for prompt-injection / credential / invisible-Unicode patterns before acceptance; reject matches
- [x] 3.3 `internal/agent/context.go` — remove `MEMORY.md` from the shared `maxPersonaBytes` silent-truncation path; the memory middleware loads it under its own explicit cap with a truncation notice

## 4. Memory middleware (inject + flush)

- [x] 4.1 `internal/agent/middlewares/memory_middleware.go` — `BeforeAgent`: inject frozen curated core into the system prompt at session start (prefix-cache-safe); `AfterAgent`: no-op (flush happens via compaction callback or EventStop)
- [x] 4.2 Write-cursor in `msg.Extra` as `_onclaw_memcursor` (alongside `_onclaw_seq`/`_onclaw_persisted`); skip extraction when already advanced
- [x] 4.3 Flush-before-compaction: in the existing summarization `Callback` (`internal/agent/agent.go`), extract durable facts from `before ⊖ after` messages *before* `convStore.SaveSummary`; piggyback the in-flight LLM; extractive fallback when the LLM fails
- [x] 4.4 `EventStop` flush path for short sessions: `MemoryMiddleware.FlushMessages` is called from `eventIterator.Next()` on normal session end via `onStopFlush`; agent stores `memoryMiddleware` reference to wire this callback
- [x] 4.5 `memory_middleware_test.go` — frozen inject is stable across turns; flush fires on compaction; write-cursor is idempotent across retries; extractive fallback writes on LLM failure

## 5. Tools

- [x] 5.1 `internal/agent/tools/memory.go` — `memory_search` (archive, hybrid), `session_search` (FTS5 over `ConversationStore`), `memory` (add/replace/remove core); `tools.Register` + `RegisterConfig("Memory", schema, load, save)`
- [x] 5.2 FTS5 index over `conversation_messages` for `session_search` (migration in `internal/store/sqlite/db.go`)
- [x] 5.3 `memory_test.go` — search returns ranked hits; `session_search` finds past messages; `memory` add/replace/remove enforce cap + security scan; auto-seed into `tool_registry`

## 6. Wiring + config

- [x] 6.1 Append `memory_middleware` to the handler chain in `internal/agent/agent.go` `AssembleAgent` (beside `summarization`/`history`/`skill`/`hooks`)
- [x] 6.2 `memory` config block: `enabled`, embedding provider, char limit, `write_approval` (default off), weights — wired through the CLI assembly root
- [x] 6.3 Thread the memory stores + embedder through assembly (`internal/cli/context.go` / agent-session assembly)

## 7. End-to-end verification

- [x] 7.1 `make fmt && make vet && make build` — confirm `CGO_ENABLED=0`, no new storage deps (eino-ext embedding only against existing eino)
- [x] 7.2 `go test ./internal/memory/... ./internal/store/sqlite/... ./internal/agent/tools/... ./internal/agent/middlewares/...` — all pass; ≥ 70% coverage per package
- [x] 7.3 Manual: `onclaw run` across two sessions — agent recalls a fact from session 1 in session 2 without being told twice; `memory_search` and `session_search` return ranked results; overflowing `MEMORY.md` errors rather than silently truncating
