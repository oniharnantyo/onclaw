# agent-memory-retrieval Delta

## MODIFIED Requirements

### Requirement: Prefetch injection
When the gate finds a turn needs deep memory, the system SHALL retrieve a small bounded set of candidates (top-k, hard cap on injected size) from the routed buckets and inject them as cited candidates alongside the always-injected documents. Retrieved candidates SHALL carry their evidence pointers and visibility stamps. Candidates SHALL be drawn from multiple retrieval channels — lexical and vector fusion over notes and events, raw-evidence hits, and entity-graph traversal when the query routes associatively — with the channels ranked and merged into the same bounded injection; the budget is shared, not per-channel.

#### Scenario: Prefetch is bounded
- **WHEN** the gate routes a turn to retrieval
- **THEN** at most k candidates within a fixed token budget are injected, each carrying its evidence pointer

#### Scenario: Associative route draws from traversal
- **WHEN** the gate classifies a query as entity-associative (an entity linked to events whose detail is asked for)
- **THEN** the prefetch candidates include graph-traversal results for that entity alongside fused lexical/vector hits, all within the shared budget

#### Scenario: Vector channel contributes
- **WHEN** a query is a paraphrase of a stored fact with little lexical overlap
- **THEN** the vector channel surfaces the fact into the candidate set that lexical search alone would have missed

### Requirement: Memory search tool
Agents SHALL have exactly one read-only memory search tool exposing the notes and events stores with agent-settable filters (time window, visibility bucket, free-text query). Retrieval itself SHALL use hybrid lexical search (full-text plus trigram) and SHALL NOT require a model call. The tool SHALL be identity-bound to the executing run's workspace, user, and agent — no argument can widen what it searches. The tool SHALL NOT write under any circumstance.

Multi-word free-text queries SHALL select candidates matching ANY of the query's terms rather than requiring every term, and SHALL order results best-match-first by term overlap so that top-k truncation keeps the closest matches. An exact substring match on the full query SHALL still match regardless of term overlap (verbatim identifiers, quotes). This matching semantics SHALL be uniform across every consumer of the shared search path: turn-time prefetch, the notes API free-text filter, and the search tool. Every consumer of the search tool SHALL resolve the tool call to the search tool — a model addressing the search capability SHALL never fall through to skill resolution.

The tool SHALL additionally accept an optional entity filter naming a stored entity, in which case results include the entity's linked notes and events reached by traversal, and free-text retrieval SHALL fuse lexical and vector channels via rank fusion. Results MAY include raw-evidence hits (source turn text) when extracted stores hold no better match; every result — extracted, traversed, or raw — carries the same provenance tuple with its evidence pointer. Traversal and raw hits obey the caller's visible set exactly as direct hits do.

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

#### Scenario: Entity filter traverses without leaking
- **WHEN** an agent searches with an entity filter whose linked events include rows outside the caller's visible set
- **THEN** only the visible linked rows are returned, and each carries its provenance

#### Scenario: Raw evidence surfaces with citation
- **WHEN** a query matches raw turn text better than any extracted row
- **THEN** the raw hit is returned citing its source event id, and the answer can cite it under the same citation-lock rules

### Requirement: Citation lock and abstention
Answers SHALL only cite memory evidence that was actually opened during retrieval for that turn — a row merely existing in the store is not citable. When retrieval and search open nothing relevant, the agent's answer SHALL state that nothing is recorded rather than fabricating recalled content. Evidence opened through the vector channel, entity traversal, or raw-evidence hits SHALL be citable under the same rule: the evidence pointer delivered with the result is the citation.

#### Scenario: Only opened evidence is cited
- **WHEN** an answer relies on stored memory
- **THEN** every memory-backed claim traces to an evidence pointer that was opened during this turn's retrieval

#### Scenario: Nothing found means nothing claimed
- **WHEN** a question has no matching memory and no opened evidence
- **THEN** the agent states nothing is recorded rather than guessing

#### Scenario: Traversed and raw evidence cite like direct evidence
- **WHEN** an answer uses a fact that arrived via graph traversal or a raw-evidence hit
- **THEN** the claim cites that result's evidence pointer — the linked row's or the raw turn's source event id — with no additional citation requirements
