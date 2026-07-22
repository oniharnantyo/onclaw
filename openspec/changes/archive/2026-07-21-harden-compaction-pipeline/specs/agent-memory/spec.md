## ADDED Requirements

### Requirement: Compaction-time extraction respects the extraction flag
A disabled extraction SHALL perform no archival at every call site, including the compaction path: the summarization callback SHALL gate any compaction-time `ExtractAndFlush` on the agent's resolved extraction-enabled flag, exactly as the turn-end flush path does. This reinforces the existing requirement that a disabled extraction stops archival for that agent, and closes the path by which compaction extracted (and attempted to embed) facts even with all memory disabled.

#### Scenario: Compaction does not extract when extraction is disabled
- **WHEN** a conversation compacts with the agent's extraction disabled
- **THEN** no facts are extracted and no documents are written to the memory archive

### Requirement: Embedding degrades to FTS-only when no model is configured
The embedder SHALL NOT call the underlying embedding provider when no embedding model is configured (empty model name). Instead it SHALL return no vector so the document is indexed for full-text search only, rather than failing with a provider error. This applies whenever extraction runs without a configured embedding model.

#### Scenario: An unconfigured embedder does not error
- **WHEN** extraction produces a fact but no embedding model is configured
- **THEN** the document is indexed without a vector and no `400 model field is required` error is raised