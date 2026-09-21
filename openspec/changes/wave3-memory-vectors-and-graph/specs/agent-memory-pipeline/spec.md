# agent-memory-pipeline Delta

## ADDED Requirements

### Requirement: Raw evidence embedding
At turn end the pipeline SHALL embed the raw turn text into the evidence index asynchronously, before extraction quality can matter — the index-before-extract guarantee that a failed or skipped extraction never leaves a fact unretrievable. Each raw embedding row SHALL carry a visibility snapshot derived from the session's participant shape at ingestion time (the same rule the gister applies) and the source event id as its permanent citation pointer. The stage SHALL be fail-soft: an embedding failure skips the row and the pipeline continues, never failing the run.

#### Scenario: Failed extraction loses nothing
- **WHEN** the curation gate skips or fails on a turn whose raw text was embedded
- **THEN** the fact remains retrievable through the raw-evidence channel with its source event id

#### Scenario: Raw rows respect session ceiling
- **WHEN** a raw turn from a single-human direct chat is embedded
- **THEN** its index row is stamped at the session's visibility ceiling and is invisible to members who could not read the session

### Requirement: Extracted-row embeddings
When the gister and curation gate commit events and notes, the pipeline SHALL embed each committed row using the workspace's configured embedding model and dimension, storing the dimension alongside the vector. The stage SHALL be fail-soft and batched per ingestion job; rows without embeddings remain fully retrievable through the lexical channel. A workspace embedding-model change SHALL take effect for new rows only — existing rows keep their stored dimension and stay lexically retrievable.

#### Scenario: Committed rows become vector-retrievable
- **WHEN** an ingestion job commits notes and events with a workspace embedding model configured
- **THEN** each committed row gains an embedding at the configured dimension retrievable by the fusion search

#### Scenario: Mixed dimensions degrade gracefully
- **WHEN** a workspace changes its embedding model and a query runs against rows embedded at the old dimension
- **THEN** old-dimension rows are excluded from the vector channel and still surface through the lexical channel

### Requirement: Entity resolution at extraction
The curation gate's existing extraction side-call SHALL additionally propose entity mentions (people, projects, systems, vendors) and their links to the notes it commits, and the gister SHALL link committed events to the entities they mention. Entity resolution SHALL ride the same model calls as extraction — zero additional side-calls — and SHALL be fail-soft: a malformed or missing entity proposal skips that proposal without rejecting the batch. Workspace posture does not widen entities: entity labels are tenant-scoped, and access control lives entirely on the edges (narrowest-visibility inheritance).

#### Scenario: Notes arrive with entity links
- **WHEN** the gate commits a note that mentions a recognized entity
- **THEN** an edge connects the note to the entity in the same ingestion job, with no additional model call

#### Scenario: Malformed proposal skips alone
- **WHEN** the extraction response contains one malformed entity proposal among valid ops
- **THEN** that proposal is skipped individually and the valid ops commit as usual
