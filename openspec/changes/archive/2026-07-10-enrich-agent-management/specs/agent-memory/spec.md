## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Memory writes are security-scanned before injection

Because memory is injected into the system prompt, the system SHALL scan every memory write for prompt-injection, credential-exfiltration, and invisible-Unicode patterns before accepting it. The scan SHALL be configurable per agent and SHALL default to enabled; when an agent disables it the UI SHALL display a prominent warning. Persona-file writes performed through the UI SHALL be scanned regardless of this per-agent setting. A write matching a threat pattern SHALL be rejected.

#### Scenario: A prompt-injection payload is rejected

- **WHEN** a memory write contains content matching a known injection or exfiltration pattern
- **THEN** the write is rejected and no memory is persisted

#### Scenario: Disabling the scan surfaces a warning

- **WHEN** an agent's memory configuration disables the security scan
- **THEN** the agent's memory configuration UI shows a warning describing the injection and exfiltration risk
