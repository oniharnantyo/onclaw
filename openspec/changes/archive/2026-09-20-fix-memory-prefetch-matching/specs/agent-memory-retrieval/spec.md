# agent-memory-retrieval Delta

## MODIFIED Requirements

### Requirement: Memory search tool
Agents SHALL have exactly one read-only memory search tool exposing the notes and events stores with agent-settable filters (time window, visibility bucket, free-text query). Retrieval itself SHALL use hybrid lexical search (full-text plus trigram) and SHALL NOT require a model call. The tool SHALL be identity-bound to the executing run's workspace, user, and agent — no argument can widen what it searches. The tool SHALL NOT write under any circumstance.

Multi-word free-text queries SHALL select candidates matching ANY of the query's terms rather than requiring every term, and SHALL order results best-match-first by term overlap so that top-k truncation keeps the closest matches. An exact substring match on the full query SHALL still match regardless of term overlap (verbatim identifiers, quotes). This matching semantics SHALL be uniform across every consumer of the shared search path: turn-time prefetch, the notes API free-text filter, and the search tool. Every consumer of the search tool SHALL resolve the tool call to the search tool — a model addressing the search capability SHALL never fall through to skill resolution.

#### Scenario: Agent searches on demand
- **WHEN** an agent calls the search tool with a query and a time-window filter
- **THEN** it receives matching notes and events with provenance, without any ingestion or model side-effect

#### Scenario: Search cannot cross identity
- **WHEN** an agent executing for one member searches memory
- **THEN** results exclude every other member's user-visibility rows regardless of query content

#### Scenario: Partial term overlap still retrieves
- **WHEN** a multi-word query shares only some of its terms with a stored note (e.g. the query names "payment provider billing" and the note says "billing has been migrated")
- **THEN** the note remains in the result set, ranked by how many terms it matches, instead of being excluded for the missing terms

#### Scenario: Ranking keeps the best match in top-k
- **WHEN** a prefetch-sized candidate limit truncates the results of a multi-word query
- **THEN** notes matching more query terms appear ahead of notes matching fewer, so the bounded injection carries the closest matches

#### Scenario: Verbatim phrase still matches
- **WHEN** the query text appears verbatim inside a note's content but its terms are not a good lexical match (e.g. an identifier or a quoted phrase)
- **THEN** the note is still returned via the exact-substring fallback

#### Scenario: Search tool resolves to the search tool
- **WHEN** a model call addresses the memory search capability on an agent whose workspace has the memory tool enabled
- **THEN** the call resolves to the memory search tool and returns results, never a skill-not-found error
