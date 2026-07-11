# agent-memory Specification

## Purpose
TBD - created by archiving change add-agent-memory-core. Update Purpose after archive.
## Requirements
### Requirement: Curated core memory is bounded and system-curated

The system SHALL maintain a curated long-term memory core (`MEMORY.md`) of durable facts,
preferences, and decisions. The core SHALL be loaded into the system prompt once at session start
and SHALL NOT be mutated mid-session. A write that would exceed the configured character cap SHALL
be rejected with an error rather than silently truncated; the caller SHALL consolidate or remove
existing entries and retry within the same turn.

#### Scenario: The core is injected once per session

- **WHEN** an agent session starts
- **THEN** the curated core is loaded into the system prompt and is not re-read on subsequent turns

#### Scenario: An overflowing write is rejected, not truncated

- **WHEN** a core write would exceed the character cap
- **THEN** the write is rejected with a descriptive error and usage, and the on-disk core is unchanged

### Requirement: Memory writes are security-scanned before injection

Because memory is injected into the system prompt, the system SHALL scan every memory write for prompt-injection, credential-exfiltration, and invisible-Unicode patterns before accepting it. The scan SHALL be configurable per agent and SHALL default to enabled; when an agent disables it the UI SHALL display a prominent warning. Persona-file writes performed through the UI SHALL be scanned regardless of this per-agent setting. A write matching a threat pattern SHALL be rejected.

#### Scenario: A prompt-injection payload is rejected

- **WHEN** a memory write contains content matching a known injection or exfiltration pattern
- **THEN** the write is rejected and no memory is persisted

#### Scenario: Disabling the scan surfaces a warning

- **WHEN** an agent's memory configuration disables the security scan
- **THEN** the agent's memory configuration UI shows a warning describing the injection and exfiltration risk

### Requirement: A searchable memory archive supports hybrid recall

The system SHALL maintain a searchable memory archive (`memory_documents`) indexed with both FTS5
and vector embeddings. `memory_search` SHALL rank results by a hybrid score combining FTS and
vector similarity. Vectors SHALL be stored as BLOBs with similarity computed in Go, with no CGO
vector extension. Embeddings SHALL be produced by the eino-ext embedding component
(`github.com/cloudwego/eino-ext/components/embedding/<provider>`, e.g. `openai`, `gemini`,
`ollama`), reusing the same provider ACL surface as the model components onclaw already depends
on. The system SHALL NOT ship a bespoke `net/http` embedding client.

#### Scenario: Hybrid search ranks a semantically-close memory above a keyword-only match

- **WHEN** the agent searches the archive with a query whose wording differs from the stored memory
- **THEN** the vector channel surfaces the memory and it ranks in the results

#### Scenario: Search degrades to FTS when the embedding provider is unreachable

- **WHEN** the embedding provider is unavailable for a search
- **THEN** the search still returns FTS-ranked results rather than failing

### Requirement: Memory extraction flushes before compaction

The system SHALL extract durable facts from messages about to be removed by compaction before the
compaction summary is persisted. Extraction SHALL be idempotent across retries via a write-cursor
stored in message metadata. A session that ends without reaching the compaction threshold SHALL
still be flushed on session stop.

#### Scenario: Facts are flushed before a compaction summary is saved

- **WHEN** the summarization middleware compacts a range of messages
- **THEN** durable facts from that range are written to memory before the summary is persisted

#### Scenario: A retried turn does not double-extract

- **WHEN** extraction runs again over a range already covered by the write-cursor
- **THEN** no duplicate memory is written

### Requirement: Memory tools are available to the agent

The system SHALL provide `memory_search` (archive), `session_search` (past conversations via FTS5),
and `memory` (add/replace/remove curated-core entries) tools. The tools SHALL auto-seed into the
tool registry as enabled by default.

#### Scenario: The agent searches its own past

- **WHEN** the agent calls `session_search` with a query
- **THEN** matching past conversation messages are returned, ranked by FTS5 relevance

### Requirement: Each agent carries its own memory configuration

The system SHALL store a per-agent memory configuration as a JSON document on the agent row (`agents.memory_config`), composed over the global `memory.*` configuration at assembly time. Unset (zero or empty) per-agent values SHALL fall back to the corresponding global value, so an agent with an empty configuration behaves identically to the global defaults. A configuration document that fails to parse SHALL be logged and replaced by the defaults rather than failing session start.

#### Scenario: An agent with no per-agent config uses global defaults

- **WHEN** an agent's `memory_config` is empty or `{}`
- **THEN** the agent's effective memory configuration equals the global `memory.*` configuration

#### Scenario: A corrupt config does not block startup

- **WHEN** an agent's `memory_config` is not valid JSON
- **THEN** the system logs a warning, uses the default per-agent configuration, and the agent session starts normally

### Requirement: Memory features are individually toggleable per agent

The system SHALL allow each memory feature to be enabled or disabled independently per agent: core-memory injection, extraction and archival, retrieval (`memory_search`/`session_search`), episodic summarization, knowledge-graph extraction, dreaming/consolidation, and staged-write approval. A disabled feature SHALL perform none of its operations for that agent, even when the backing store exists. Feature toggles SHALL be AND-combined with the relevant global enabled flag (for example, the knowledge-graph feature SHALL remain off when global `memory.kg_enabled` is false).

#### Scenario: Disabling extraction stops archival for that agent only

- **WHEN** an agent has `extraction` disabled and runs a conversation
- **THEN** no new documents are written to the memory archive for that agent, while other agents continue to extract normally

#### Scenario: Disabling retrieval withholds the search tools

- **WHEN** an agent has `retrieval` disabled
- **THEN** the `memory_search` and `session_search` tools are not available to that agent

### Requirement: An agent may select its own embedding model

The system SHALL allow each agent to specify its embedding provider and model, overriding the global embedding configuration. Per-agent embeddings SHALL be cached under a key that includes the embedding model so that agents using different models do not share cached vectors. Each archived memory document SHALL record the embedding model that produced its vector, and archive search SHALL filter to documents matching the agent's current embedding model, so that changing an agent's embedding model does not mix vector dimensions within that agent's partition.

#### Scenario: Two agents with different embedding models do not collide in the cache

- **WHEN** agent A embeds text with model X and agent B embeds the same text with model Y
- **THEN** each retrieves its own correct cached vector and neither returns the other's

#### Scenario: Search ignores vectors from a prior embedding model

- **WHEN** an agent's embedding model changes after documents were indexed with a different model
- **THEN** archive search for that agent excludes documents indexed under the old model

