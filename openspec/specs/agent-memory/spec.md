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

Because memory is injected into the system prompt, the system SHALL scan every memory write for
prompt-injection, credential-exfiltration, and invisible-Unicode patterns before accepting it. A
write matching a threat pattern SHALL be rejected.

#### Scenario: A prompt-injection payload is rejected

- **WHEN** a memory write contains content matching a known injection or exfiltration pattern
- **THEN** the write is rejected and no memory is persisted

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

