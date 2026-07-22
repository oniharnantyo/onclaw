# Tasks

Implementation is phased; each phase is independently shippable and verifiable. Phases 0–3 map to
the layers in `design.md`.

## Phase 0 — Replay reasoning-strip + summarizer scrub + prompt (Layers A & B)

- [x] 0.1 Strip `ContentBlockTypeReasoning` from messages loaded in `HistoryMiddleware.BeforeAgent`
  (replay path); add black-box test asserting no reasoning block is injected into `AgentInput.Messages`.
- [x] 0.2 Stop persisting reasoning blocks: filter them in `accumulateNewMessages` so new turn rows
  carry no reasoning. Confirm Langfuse/observability still captures reasoning via the event path.
- [x] 0.3 Implement summarizer-input scrubbing via Eino's `GenModelInput`: for the range being
  summarized, replace old tool-result content with stubs (Decision 4 taxonomy), trim file-write
  tool-call args (keep path, drop `content`/`new_string`), strip reasoning/mechanics. Anchor to
  `summary_until_seq`.
- [x] 0.4 Author the tailored recall-first `UserInstruction` (Decision 5) and wire it in
  `buildSummarizationConfig`.
- [x] 0.5 Black-box tests: stub taxonomy (path-preserving, skill-preserving, generic), arg trimming,
  reasoning absent from summarizer input. Verify provider-pair integrity (no orphaned tool
  call/result after scrubbing).
- [x] 0.6 Verify: `make vet`, `go test ./internal/agent/... ./internal/store/sqlite/...`,
  `make build`; openspec validate.

## Phase 1 — Prefix caching per adapter (Layer D)

- [x] 1.1 Audit each adapter for caching capability (Anthropic `CacheControl`, OpenAI automatic,
  Gemini context cache, Ollama/openai-compatible none).
- [x] 1.2 Enable Anthropic `CacheControl` in `agentic_claude.go`; confirm the live prefix (system +
  history + tools) is marked cacheable and stable (Decision 6).
- [x] 1.3 Confirm OpenAI/openai-compatible benefit from automatic prefix caching given the stable
  verbatim prefix; document that no opt-in is needed.
- [ ] 1.4 Implement Gemini context caching where the adapter supports it.
      (DEFERRED: only the cache *expiration policy* is set in `agentic_gemini.go`; the cached-content
      resource is not created and no request references it, so Gemini re-bills the prefix — caching is
      effectively OFF. Tracked by the `complete-prefix-caching` follow-up change.)
- [x] 1.5 Tests + a config flag to disable caching for providers where it misbehaves.

## Phase 2 — Files-first recovery + lazy transcript (Layer C)

- [x] 2.1 Track file-access recency: observe filesystem-tool calls (`read_file`/`write_file`/`edit_file`/
  `glob`/`grep`) into a per-conversation recency list (in-memory, spilled to KV).
- [x] 2.2 After compaction, inject the freshest N (default 5) file paths alongside the summary as a
  compact "recently accessed files" note; the agent re-reads on demand.
- [x] 2.3 Make the transcript lazy: stop writing on every compaction; build `Transcript(upToSeq)` and
  write the file only when the agent reads the transcript path.
- [x] 2.4 Demote transcript references in the summary prompt to "fallback"; update `UserInstruction`.
- [x] 2.5 Tests for recency tracking, post-compaction injection, and lazy transcript build-on-read.

## Phase 3 — Probe-based evaluation harness (Layer E)

- [x] 3.1 Capture representative real agent traces as fixtures (with secrets redacted).
- [x] 3.2 Implement recall probes (path / decision / unresolved-bug / skill / identifier preserved)
  and precision probes (redundant tool body dropped).
- [x] 3.3 Score across the six dimensions; rotate probe sets to avoid blind spots.
- [x] 3.4 Use results to tune `UserInstruction` (dial the recall-heavy closing line down as precision
  improves) and the clearing policy; record a baseline.

## Cross-cutting

- [x] C.1 Update `openspec/specs/conversation-history/spec.md` from this change's delta on archive.
- [x] C.2 Keep bounded-replay and append-only retention contracts unchanged (regression tests).
- [x] C.3 Document the cache-stability invariant (no per-turn mutation of replayed messages) in code
  comments at the scrub sites.
