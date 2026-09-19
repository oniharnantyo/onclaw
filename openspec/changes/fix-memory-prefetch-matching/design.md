# Design: fix-memory-prefetch-matching

## Context

Live evidence from the `fix-memory-retrieval-lane` live pass (2026-09-19, all disk/live-verified):

- The matcher: `internal/store/postgres/memory_notes.go` `queryNotes` matches with `to_tsvector('english', content) @@ plainto_tsquery('english', $N) OR content ILIKE '%' || $N || '%'`. `plainto_tsquery` ANDs every term and the `ILIKE` leg needs the whole raw string as a literal substring. As budi (32 visible notes): `q=billing` → 7, `q=Midtrans` → 2, `q=payment provider billing` → 0 despite the note "…customer billing has been migrated…" containing "billing" verbatim. The events search mirrors the same construction, and the in-memory fake (`internal/store/fake`) whole-string-`Contains`es the query — all three legs share the starvation semantics.
- `Searcher.Prefetch` (`internal/memory/search.go:124`) passes the raw turn text into `SearchNotes`/`SearchEvents` unshaped, so turn-time injection inherits the semantics directly.
- The self-search leg: a live turn's tool call came back `skill not found: memory` (raised at `internal/agents/backend/skill_backend.go:208`) — the tool lookup failed and the call fell through to skill resolution. Root cause not yet isolated (candidates: the fixture agent's exposed-tools config, the workspace tool-settings enabled bit for the memory tool, or the registry's call-name → tool-key mapping).

Constraints carried over: the gate/prefetch path stays fail-open and bounded (top-k + char budget unchanged); scope filtering (workspace/user/agent visibility clause) is untouched — matching only changes WHICH visible rows qualify and their order; no schema or data migration.

## Goals / Non-Goals

Goals:
- A multi-word query no longer zeroes out a bucket because one term is missing; best matches rank first so top-k truncation keeps them.
- One matching semantics across prefetch, the notes API `q`, and the agent search tool — fixed once, in the store.
- The model's self-search call resolves to the search tool on a workspace with the memory tool enabled.
- A recorded pre-wave-3 scoreboard that measures memory quality rather than phrasing luck.

Non-Goals:
- Embedding/vector retrieval, graph traversal, or any wave-3 scope.
- Changing the candidate bounds (top-k, injected-char budget) or the fail-open polarity.
- Natural-language query understanding (no model call in the search path, per the existing requirement).
- Re-scoring old runs; the baseline history stands as recorded.

## Decisions

- **D1: Fix the matcher in the store, not query shaping in the caller.** `SearchNotes`/`SearchEvents` (postgres) and the fake gain any-term semantics at the single chokepoint every consumer shares — prefetch, the notes API `q`, and the tool all cure at once. Alternative considered: keyword extraction in `Prefetch` only (rejected — leaves the search tool and the API starved, and forks a second tokenizer).
- **D2: OR-of-terms via an explicit sanitized `to_tsquery`.** The query is tokenized (whitespace/punctuation split, lowercased, empties and tsquery operator characters stripped, capped at ~16 terms), the surviving terms joined with `|`, and matched as `to_tsvector('english', content) @@ to_tsquery('english', 't1 | t2 | …')`. The full-string `ILIKE '%' || $N || '%'` fallback is retained verbatim for exact identifiers/quotes. Alternative considered: `websearch_to_tsquery` (rejected — its OR requires the caller to type the syntax, which a raw turn text never does) and keeping `plainto_tsquery` (is the bug).
- **D3: Rank-ordered results with pinning still dominant.** Ordering becomes `pinned DESC, ts_rank(…) DESC, learned_at DESC, id DESC`: a curation pin outranks lexical quality, term-overlap rank decides among equals, recency breaks ties. This is what makes OR safe for top-k: the bounded candidate set keeps the closest matches instead of the newest.
- **D4: The fake mirrors the semantics, not Postgres internals.** The fake's `queryNotes` matches when ANY lowercased term (same tokenizer: split, lowercase, strip empties) is a substring of the content, keeps the exact whole-string substring hit as always-matching, and orders by descending matched-term count before its existing order. Both stores get the same multi-word table test (postgres copy under the integration tag), so the parity contract is executable.
- **D5: Events search gets the identical construction** (same tokenize/join/rank shape against the events table), so routed-bucket prefetch cannot starve on one leg while the other fires.
- **D6: The self-search leg is diagnose-then-fix, not blind rewiring** (mirrors the Langfuse repair's discipline). Spike order: (1) what tool keys the runner's registry actually registers for memory (`memory.search` vs `memory`) and what name the model emitted; (2) whether the fixture agent's exposed-tools config includes the tool; (3) whether the workspace tool-settings row for the memory tool is enabled on `memory-eval`; (4) where a failed tool lookup falls through to skill resolution and whether it should 422-style error instead. The fix lands wherever the evidence names the layer — fixture provisioning, policy gate, or registry mapping — and the delta's "resolves to the search tool" scenario is the acceptance. If the root cause is eval-fixture config only, no production code changes and the change records that outcome.
- **D7: Scoreboard rerun closes the change.** Fresh run id on the reused `memory-eval` fixture (gate budget stays at the live-verified 8000; the 4s-vs-8s question belongs to the previous change's record), scoreboard JSON in the change folder, and the delta summarized against eval-20260919-194224. That run is the pre-wave-3 baseline.

## Risks / Trade-offs

- [OR widens result sets; precision drops on noisy terms like "what"/"team"] → term cap plus rank ordering keeps the top-k relevant; stopword handling rides the english text-search configuration, and the injected char budget bounds any damage. The scenario contract pins rank ordering, not raw recall.
- [Long turn texts produce many terms] → the ~16-term cap bounds the tsquery; the remainder of the turn still has the ILIKE fallback and the scope clause doing its job.
- [Fake and postgres ranking cannot be identical (no ts_rank in memory)] → parity is pinned on membership (any-term) and coarse ordering (more matches first), not identical rank values; the table tests assert exactly that contract.
- [D6's root cause may be production (registry fallthrough) rather than fixture config] → the spike names the layer before any edit; a production fix must preserve the property that an unknown tool surfaces a real error, never silently degrades into skill execution.
- [Changing the API `q` behavior alters user-visible search results] → strictly more results, best first; consumers (settings UI search, eval harness) are tolerant, and the requirement text now states the semantics explicitly.

## Migration Plan

1. Land the store matcher (postgres + fake + tests) — pure behavior change inside the existing search contract; no migration.
2. Run the D6 spike; land the named fix (production or fixture) with its regression test.
3. Rerun the scoreboard with a fresh run id; record JSON + delta in the change folder.
4. Rollback: each piece reverts independently; no data, schema, or wire changes to unwind.
