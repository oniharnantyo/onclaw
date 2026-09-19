# Proposal: fix-memory-prefetch-matching

## Why

The post-budget scoreboard (eval-20260919-194224, recorded in `fix-memory-retrieval-lane`) proved the gate now completes and prefetch injects, but live probing exposed that retrieval matching itself starves on multi-word queries: `SearchNotes` matches with `plainto_tsquery` (which ANDs every word) plus a whole-string `ILIKE` fallback, so a turn like "what payment provider did we decide on" scores zero against a note containing "billing" verbatim. Prefetch passes the raw turn text straight into this matcher, so injection only fires on phrasing luck — one shared word short of total starvation. Separately, the fixture agent's explicit memory search attempt fails with `skill not found: memory` (the call falls through to the skill backend), meaning the self-search leg is dead and all evidence flows through prefetch alone. Both defects depress the eval numbers that gate wave-3, so the scoreboard cannot honestly measure memory quality until they land.

## What Changes

- Retrieval matching semantics become any-term with rank ordering: a query's terms are ORed for candidate selection and results are ordered by match quality (term overlap via `ts_rank`), instead of requiring every term to match. The pinned top-k candidate sets and the search API/tool results both get rank-ordered best-matches-first behavior. The exact-substring `ILIKE` fallback is retained for verbatim identifiers.
- The matcher fix applies uniformly to the notes store, the events store, the fake store (parity), and every consumer of the shared search path: turn-time prefetch, the notes API `q` filter, and the agent-facing memory search tool.
- The `skill not found: memory` failure is diagnosed (tool registry key vs the model's call name, the fixture agent's exposed tools, the workspace tool-settings enabled bit) and repaired so the model's self-search leg resolves to the memory search tool. This is conformance repair against the existing Memory search tool requirement — no requirement change.
- The scoreboard is rerun on the fixed lane with a fresh run id and recorded in the change folder; that run is the pre-wave-3 baseline.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-memory-retrieval`: the Memory search tool requirement gains explicit matching semantics — multi-word queries select candidates matching ANY query term, ranked by term overlap, instead of requiring every term; scoped to the shared lexical search that prefetch, the notes API, and the tool all consume.

## Impact

- `internal/store/postgres/memory_notes.go` `SearchNotes`/`queryNotes` (tsquery construction + ordering), `internal/store/postgres/memory_events.go` event search (same matcher), `internal/store/fake` search parity, and their tests.
- `internal/memory/search.go` `Prefetch` (only if query shaping moves to the caller; the default plan keeps shaping inside the store).
- Tool-resolution path for the self-search leg: `internal/agents` tool registry/policy and the eval fixture's agent provisioning in `internal/memory/eval` (fix depends on the spike's findings).
- No wire-format or schema changes; no data migration. The notes API `q` parameter's observable behavior changes for multi-word queries (more results, best matches first) — call sites are the settings UI search and the eval harness, both tolerant.
