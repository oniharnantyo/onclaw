# Tasks: fix-memory-prefetch-matching

## 1. Notes matcher (postgres)

- [x] 1.1 Add a shared query-shaping helper for the notes/events stores: tokenize a query (whitespace/punctuation split, lowercase, drop empties and tsquery operator characters, cap at 16 terms), build the `t1 | t2 | …` tsquery string, and expose the matched-term count for ordering
  - `internal/store/postgres/memory_query.go` `shapeMemoryLexicalQuery` (`[a-z0-9]+` token class keeps every tsquery operator outside; 16-term cap; zero-term shape signals callers to skip the tsquery leg); unit table `memory_query_test.go`
- [x] 1.2 `internal/store/postgres/memory_notes.go` `queryNotes`: replace the `plainto_tsquery` leg with the OR tsquery, keep the full-string `ILIKE '%' || $N || '%'` fallback, and change ordering to `pinned DESC, ts_rank(...) DESC, learned_at DESC, id DESC` (rank computed over the OR tsquery; ILIKE-only hits rank last)
  - joined tsquery string bound as a parameter (no token interpolation); ILIKE fallback verbatim; scope/superseded/LIMIT clauses byte-identical (verifier diff)
- [x] 1.3 Integration tests (postgres tag): multi-word partial-overlap table test — "payment provider billing" retrieves the "billing has been migrated" note; rank ordering puts the more-terms match ahead; verbatim substring still matches when terms don't; single-word behavior unchanged; empty-after-tokenization query falls back to the ILIKE leg rather than erroring
  - `memory_match_integration_test.go` `TestIntegration_MemorySearch_NotesMultiWordMatching` 5/5 vs live Postgres (incl. verbatim `ops@service-now.com` ILIKE-only hit)
- [x] 1.4 Run the notes API `q=` filter through the same path live (curl as the corpus owner: multi-word query that previously returned 0 now returns the note; scoped visibility unchanged)
  - live 2026-09-19: budi `q=payment provider billing` → 5 hits (previously 0), `q=billing` → 7 (baseline); sari same multi-word query → 0 (budi's private rows stay invisible)

## 2. Events matcher and fake parity

- [x] 2.1 `SearchEvents` (postgres): the identical tokenize/join/rank construction against the events table, same ILIKE fallback and tie-break ordering; integration-table test mirroring 1.3
  - `memory_notes.go` queryEvents: rank over `description || ' ' || outcome`, both ILIKE legs kept, `ts_rank DESC, event_time DESC, learned_at DESC, id DESC` (events have no pinned column); `TestIntegration_MemorySearch_EventsMultiWordMatching` 5/5
- [x] 2.2 Fake store parity: `internal/store/fake` notes and events `queryNotes` match when ANY tokenized term is a lowercased substring of the content (whole-string substring still always matches), ordered by descending matched-term count before the existing order; the 1.3 table test duplicated against the fake so both stores assert the same membership/ordering contract
  - `fake.go` `memorySearchQuery`/`memorySearchMatch` (english stopwords + 16-cap mirrored); shared 7-case table in `memory_search_test.go` run against BOTH stores
- [x] 2.3 Unit suite green: `go build ./... && go vet ./... && go test ./...` with the fake tests proving multi-word retrieval that previously starved (the fake test doubles as the fake-based regression for consumers)
  - verifier fresh run: build/vet exit 0, `go test -count=1 ./...` 38/38 packages ok

## 3. Self-search leg (diagnose first)

- [x] 3.1 Spike: determine why a model's memory-search call fell through to skill resolution — enumerate the runner registry's actual memory tool keys, the name the model emitted on the recorded turn, the fixture agent's exposed-tools config, and the `memory-eval` workspace tool-settings enabled bit; name the failing layer with evidence (code refs + the stored call)
  - VERDICT: eval-fixture provisioning only. Live turn emitted `FUNCTION_CALL name=skill args={"skill":"memory"}` because the fixture agent stored `tools: []` (CreateAgent posts no tools field → handler default `[]` → runner's empty-allowlist-means-no-tools rule) — zero registry tools exposed, so the model could only reach the skill middleware's generic `skill` tool. Registry keys fine (`memory.search` + `memory` wired at both composition roots); tool-settings enabled bit fine (`memory` enabled, no disabled rows); no silent tool→skill fallthrough exists (the skill call was model-initiated; unknown names surface real errors)
- [x] 3.2 Fix the named layer: fixture provisioning (eval harness exposes the tool), tool policy/registry mapping (production fix with a regression test — an unknown tool must error, never silently become a skill), or settings enablement; if the root cause is fixture config only, record that outcome here with no production change
  - Fixture-config-only outcome: eval `CreateAgent` posts `tools:["memory.search"]`; `ensureAgent` reuse path repairs pre-existing toolless agents (one PATCH); regression `TestFixtureAgentExposesMemorySearch`. Zero production code changed; D6 property (unknown tool → real error, never silent skill execution) confirmed intact
- [x] 3.3 Acceptance: a live turn where the model calls the search capability resolves to the memory search tool and returns results with provenance (no `skill not found: memory`)
  - live 2026-09-19 (post-fix, new build): turn "What payment provider did we decide on for customer billing?" → `FUNCTION_CALL name=memory.search` → structured results with source event ids; answer cites Midtrans migration with two source event ids (no `skill not found`)

## 4. Scoreboard rerun and records

- [x] 4.1 Rerun `go run . eval-memory --email … --password … --model glm-5.3-flash` on the reused `memory-eval` fixture after 1–3 land, fresh run id (fixture keeps `gate_budget_ms=8000`)
  - run `eval-20260919-215922` on reused fixture (agent repaired to `tools:["memory.search"]` by the seed reuse path), model glm-5.3-flash, exit 0
- [x] 4.2 Record the scoreboard JSON in the change folder and summarize the delta against eval-20260919-194224 (recall/citation/abstention movement; whether multi-word turns now retrieve; self-search leg status)
  - scoreboard: `eval-scoreboard.json` (this folder). Delta vs eval-20260919-194224: recall 28.6%→**100%** (all five previously-starving questions flipped: q-recall-db, q-recall-invoice, q-update-payments, q-multihop-airport, q-multihop-leave), citation_valid 50%→**100%**, scope_safe 100%→100%, overall 61.3%→**83.3%**. Self-search leg live (memory.search calls return provenance-cited results). Abstention 66.7%→33.3% is a GRADER ARTIFACT, not behavior: both flagged answers are prose-correct abstentions — (a) `noRecordPhrases` (internal/memory/eval/score.go:30) misses the model's wordings ("Not in memory", "Nothing usable is recorded", "Nothing captures"); (b) `fabricatedLargeNumberRe` (score.go:69, `\d{7,}`) matches the digit run inside cited event UUIDs (e.g. `…83350462298e` → 11 digits → false "fabricates a specific amount"). This also root-causes the previous run's unexplained q-abstain-offsite flip. Harness grader refinement is the follow-up lane item (out of this change's scope)
- [x] 4.3 Update `docs/memory-system.md` where retrieval matching semantics are described (if the doc pins the AND behavior anywhere)
  - doc pinned the old behavior nowhere; one sentence added to the Retrieval section stating any-term matching, term-overlap ranking (pins dominant), exact-substring fallback

## 5. Validation

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green; fake/postgres parity tests present for both stores
  - verifier fresh: build/vet exit 0; `go test -count=1 ./...` 38/38 ok; `-tags=integration` memory suite 10/10 vs live Postgres; `TestMemorySearch_CannotCrossIdentity` green 25/25 (any-term cannot trip its leak heuristic on in-identity rows)
- [x] 5.2 Smoke pass: one live turn whose phrasing previously starved injection now shows prefetch injection (cited evidence in the answer / trace), and the 4.2 scoreboard is attached to the change before archive
  - live 2026-09-19 on the new build: "What payment provider did we decide on for customer billing?" (previously 0-hit phrasing) → correct Midtrans answer citing source event ids, model invoked `memory.search` itself; "Remind me where our staging database is hosted these days?" → pure-prefetch answer (NO tool call) assembled from injected candidates, citing three source event ids (`event://…` links). Scoreboard attached (eval-scoreboard.json)
