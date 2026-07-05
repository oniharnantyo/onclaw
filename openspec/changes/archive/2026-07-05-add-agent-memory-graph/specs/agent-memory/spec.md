## ADDED Requirements

### Requirement: The memory system maintains a temporal knowledge graph

The system SHALL maintain a knowledge graph of entities and typed relations, each carrying
`valid_from` and a nullable `valid_until`. A fact that changes SHALL be superseded — the old row's
`valid_until` SHALL be set and a new row inserted — never deleted in place. Default retrieval SHALL
skip rows whose `valid_until` is set.

#### Scenario: A changed fact is superseded, not overwritten

- **WHEN** an extraction produces a relation that contradicts an existing live relation between the same entities
- **THEN** the existing relation's `valid_until` is set and a new relation is inserted with `valid_from = now`

#### Scenario: Default retrieval excludes superseded facts

- **WHEN** the graph is searched without an explicit historical mode
- **THEN** rows with `valid_until` set are excluded from results

### Requirement: Entities and relations are extracted from episodic summaries

The system SHALL extract entities and relations from each new episodic summary via a structured
model call and ingest them with `valid_from = now`. Extraction SHALL run on the configured review
model and SHALL be non-fatal: an extraction error SHALL be logged and skipped, never blocking the
turn or the consolidation pipeline. Extracted text SHALL pass the memory security scan before
ingest.

#### Scenario: Extraction runs when an episodic summary is created

- **WHEN** slice #2 writes a new episodic summary
- **THEN** entities and relations are extracted from it and ingested into the graph

#### Scenario: A failed extraction does not break the turn

- **WHEN** the extraction model call fails or returns malformed output
- **THEN** the error is logged, no graph is written for that summary, and the session continues unaffected

### Requirement: The graph is deduplicated after each extraction

After each extraction batch, the system SHALL merge semantically-equivalent entities (same type +
normalized name) and re-point their relations to the surviving entity; different-type same-name
cases are left unmerged (future enhancement: ambiguous flagging on review surface).

#### Scenario: Duplicate entities are merged

- **WHEN** an extraction ingests an entity that matches an existing entity of the same type and normalized name
- **THEN** the two are merged and their relations point to the surviving entity

### Requirement: Graph search traverses relations with a bounded depth

The `kg_search` tool SHALL traverse entity relations via a bounded-depth recursive query and
return matching entity and relation paths, scoped to the agent. It SHALL auto-seed into the tool
registry as enabled by default.

#### Scenario: A relational query returns connected entities

- **WHEN** the agent calls `kg_search` for a seed entity
- **THEN** entities related within the configured depth are returned as paths
