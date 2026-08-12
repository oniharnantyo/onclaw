## MODIFIED Requirements

### Requirement: Memory tools are available to the agent

The system SHALL provide `memory_search` (archive), `session_search` (past conversations via
FTS5), `memory` (add/replace/remove curated-core entries), `memory_remember` (create an archive
document), `memory_update` (update an archive document in place), and `memory_forget` (delete an
archive document) tools. The tools SHALL auto-seed into the tool registry as enabled by default
and SHALL be managed uniformly with other tools via the standard tool enable/disable and agent
tool whitelist. `memory_search` SHALL include each result's numeric document `id` in its output
so the agent can address a specific memory with `memory_update` or `memory_forget`.

#### Scenario: The agent searches its own past

- **WHEN** the agent calls `session_search` with a query
- **THEN** matching past conversation messages are returned, ranked by FTS5 relevance

#### Scenario: Search results are addressable by id

- **WHEN** the agent calls `memory_search` and receives results
- **THEN** each result includes its document id alongside the content and relevance

### Requirement: Memory features are individually toggleable per agent

The system SHALL allow each memory **background** feature to be enabled or disabled
independently per agent: core-memory injection, extraction and archival, episodic summarization,
knowledge-graph extraction, dreaming/consolidation, retrieval auto-injection, and staged-write
approval. A disabled feature SHALL perform none of its background operations for that agent,
even when the backing store exists. Feature toggles SHALL be AND-combined with the relevant
global enabled flag (for example, the knowledge-graph feature SHALL remain off when global
`memory.kg_enabled` is false). Memory **tool availability is not gated by these toggles**: all
memory tools (`memory_search`, `session_search`, `memory_remember`, `memory_update`,
`memory_forget`) SHALL remain available to an agent regardless of the per-feature toggles,
governed only by the standard tool enable/disable and agent tool whitelist. The toggles govern
only the automatic background subsystems.

#### Scenario: Disabling extraction stops archival for that agent only

- **WHEN** an agent has `extraction` disabled and runs a conversation
- **THEN** no new documents are written to the memory archive for that agent, while other
  agents continue to extract normally

#### Scenario: Disabling retrieval stops auto-injection but keeps the search tools

- **WHEN** an agent has `retrieval` disabled
- **THEN** automatic memory auto-injection at turn start is disabled for that agent, but the
  `memory_search` and `session_search` tools remain available to that agent

## ADDED Requirements

### Requirement: The agent can write, update, and delete archive memories

The system SHALL provide `memory_remember`, `memory_update`, and `memory_forget` tools that
operate on the searchable archive (`memory_documents`). `memory_remember` SHALL create a new
document tagged `Kind="curated"`, `Scope="global"`, `Source="remember"`, and SHALL skip
insertion (returning an "already remembered" observation) when identical content already exists
for that agent. `memory_update` SHALL mutate an existing document's content in place, addressed
by its numeric `id` and preserving `id` and `created_at`, via an `UpdateDocument` store
operation that updates the content row (re-syncing the FTS index via the existing trigger) and
re-embeds the new content into `memory_embeddings` in the same transaction. `memory_forget` SHALL
delete a document by its numeric `id`. Archive search (`memory_search`), write (`memory_remember`),
update (`memory_update`), and deduplication operations SHALL pass a consistent embedding model
(matching `Embedder.ModelName` when an embedder is active, or `""` in FTS-only mode) so remembered
documents are findable under that model. `memory_remember` and `memory_update` SHALL scan content
through the memory security scanner before writing, identical to `ExtractAndFlush`. Each tool's
expected decline condition (no document with the given `id`) SHALL be returned as a recoverable
tool-result observation with no fatal error. The tools SHALL require no nil-store guard because
the `MemoryStore` is always constructed.

#### Scenario: Remembering a fact makes it searchable

- **WHEN** the agent calls `memory_remember` with a fact
- **THEN** a new archive document is created and a subsequent `memory_search` returns it

#### Scenario: Remembering identical content is a no-op

- **WHEN** the agent calls `memory_remember` with content identical to an existing document for
  that agent
- **THEN** no duplicate document is created and the tool reports the memory already exists

#### Scenario: Updating a memory changes its searchable text

- **WHEN** the agent calls `memory_update` with an existing document `id` and new content
- **THEN** the document's content is updated in place, its vector is re-embedded, a subsequent
  search reflects the new text, and the `id` and `created_at` are preserved

#### Scenario: Forgetting a memory removes it

- **WHEN** the agent calls `memory_forget` with an existing document `id`
- **THEN** the document and its embedding are deleted and a subsequent search no longer returns
  it

#### Scenario: Acting on a non-existent id is a recoverable observation

- **WHEN** the agent calls `memory_update` or `memory_forget` with an `id` that does not exist
- **THEN** the tool returns a human-readable observation that no memory has that id, makes no
  change, and returns no fatal error so the agent turn continues

### Requirement: A global embedding configuration default exists

The system SHALL provide a global embedding configuration — provider, model, and optional API
base — that is editable via the configuration UI and persisted in the preferences store. At
agent assembly, the effective embedding provider and model SHALL resolve in precedence: the
per-agent embedding override, then the global embedding configuration, then the bootstrap
`memory.embedding_*` configuration, then the agent's chat provider. An agent with no per-agent
embedding override SHALL inherit the global embedding configuration.

#### Scenario: An agent inherits the global embedding default

- **WHEN** no per-agent embedding override is set and a global embedding configuration exists
- **THEN** the agent's embedder uses the global embedding provider and model

#### Scenario: A per-agent override wins over the global default

- **WHEN** an agent defines its own embedding provider or model
- **THEN** the agent's embedder uses the per-agent values, ignoring the global default

### Requirement: Agent assembly reports the resolved embedding mode

The system SHALL log the resolved embedding provider and model (or that embeddings are disabled
and the system is running FTS-only) at agent assembly, after the embedder is constructed, so
embedding setup is observable rather than silent.

#### Scenario: An active embedding provider is logged

- **WHEN** an agent assembles with a configured, reachable embedding provider
- **THEN** assembly logs the provider and model in use

#### Scenario: FTS-only mode is logged

- **WHEN** an agent assembles with no embedding provider or key configured
- **THEN** assembly logs that embeddings are disabled and search is FTS-only
