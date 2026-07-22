## ADDED Requirements

### Requirement: Replayed history excludes reasoning content
The system SHALL strip `Reasoning`/thinking content blocks from messages loaded for replay before
each agent run, so the live context the agent reasons over never contains prior reasoning. The
strip SHALL be applied deterministically at history load and SHALL NOT mutate already-loaded
messages on subsequent turns, so the replayed prefix remains stable for prefix caching. New turn
rows SHALL NOT persist reasoning blocks into the conversation history; reasoning captured for
observability SHALL travel the event/callback path, not the replay substrate.

#### Scenario: A replayed turn drops its reasoning blocks
- **WHEN** a persisted turn carries an assistant message with both generated text and a reasoning block, and the turn is replayed into the agent
- **THEN** the injected message array contains the generated text but no reasoning block

#### Scenario: The replayed prefix is stable across turns
- **WHEN** a turn is replayed on turn N and again on turn N+1
- **THEN** the byte representation of the replayed (non-new) messages is identical across the two turns

### Requirement: Summarizer input is scrubbed of bulky mechanics
When summarization compacts a range, the system SHALL construct the summarization model's input so
that old tool-result content, reasoning, and raw tool-call mechanics within the compacted range are
removed or condensed before the summary model sees them. The scrub SHALL apply only to the range
being compacted (anchored to the coverage cursor), never to the live replay prefix, so that prefix
caching remains effective.

#### Scenario: Old tool results are condensed for the summarizer
- **WHEN** summarization runs over a range containing large tool-result bodies
- **THEN** the summarization model input contains stubs or gists in place of those bodies, not the verbatim content

#### Scenario: The live replay prefix is not mutated by summarizer scrubbing
- **WHEN** summarizer-input scrubbing is applied on a compaction
- **THEN** the messages replayed to the agent on the next turn are unchanged by the scrub (only the summary message replaces the compacted range)

### Requirement: Tool-result stubs preserve durable pointers
When a tool result is condensed for the summarizer, the system SHALL preserve a durable pointer to
the underlying artifact: for file-producing tools (`write_file`, `edit_file`, or a result
saved to a file) the stub SHALL carry the destination path verbatim; for skill invocations the stub
SHALL carry the skill name and a one-line outcome; for other tools the stub SHALL carry the tool
identity. Tool-call arguments for file-producing tools SHALL be trimmed to the path, dropping bulky
`content`/`new_string` payloads.

#### Scenario: A file write is summarized by its path
- **WHEN** the compacted range contains a `write_file` call that wrote `src/auth/login.go`
- **THEN** the summarizer input references `src/auth/login.go` and does not include the written body

#### Scenario: A prior skill use survives compaction
- **WHEN** the compacted range contains a skill invocation
- **THEN** the summarizer input records the skill name and a short outcome, so the fact and result of the skill use are preserved

### Requirement: Summarization uses a recall-first onclaw-tailored prompt
The summarization middleware SHALL be configured with a custom `UserInstruction` that instructs the
model to preserve verbatim architectural decisions and their rationale, file paths and directories,
skill uses, unresolved bugs and error messages/codes, and identifiers (UUIDs, hashes, tokens, URLs,
function/type names); to treat tool-result stubs as pointers rather than full content; to preserve
`<redacted>` secret placeholders as-is; and to discard redundant tool bodies and reasoning. The
prompt SHALL forbid tool calls during summarization.

#### Scenario: The summary preserves paths and an unresolved bug
- **WHEN** compaction summarizes a range that referenced specific file paths and an unresolved error
- **THEN** the resulting summary contains those paths and the error verbatim

#### Scenario: Redacted secrets are not reconstructed
- **WHEN** the compacted range contains `<redacted>` placeholders for secret values
- **THEN** the summary keeps them as `<redacted>` and does not invent values

### Requirement: Prefix caching is enabled per provider where supported
The system SHALL enable conversation prefix caching for providers that support it, so the stable
replayed prefix is amortized across turns rather than re-billed in full. Caching SHALL be wired per
adapter (Anthropic cache control, OpenAI-compatible automatic prefix caching, Gemini context
caching) and SHALL degrade gracefully to re-billing where a provider offers no caching. The
cache-stability invariant (no per-turn mutation of replayed messages) SHALL be preserved so caching
remains effective.

#### Scenario: A cacheable prefix is reused across turns
- **WHEN** a provider supports prefix caching and a conversation replays a stable prefix across consecutive turns
- **THEN** the provider reports cache hits on the stable portion of the prefix

#### Scenario: A non-caching provider still functions
- **WHEN** a provider offers no prefix caching
- **THEN** the agent still replays and runs correctly, re-billing the prefix

### Requirement: The agent is re-grounded with recently accessed files after compaction
After compaction, the system SHALL surface the most recently accessed workspace file paths
(default 5) alongside the summary, so the agent can re-read current source on demand rather than
relying on stale compacted copies. File-access recency SHALL be tracked from filesystem-tool usage
into a per-conversation recency list. The transcript file SHALL be demoted to a fallback recall path.

#### Scenario: Recent files follow the summary
- **WHEN** a conversation is compacted and a new turn begins
- **THEN** the agent receives the summary plus a note listing the most recently accessed file paths

### Requirement: The transcript is written lazily
The system SHALL NOT write the per-conversation transcript file on every compaction. It SHALL build
and write the transcript only when the agent (or another recall path) actually requests the
compacted range's verbatim detail. The append-only DB remains the source of truth for the
compacted range.

#### Scenario: A compaction does not eagerly write the transcript
- **WHEN** summarization compacts a range
- **THEN** no transcript file is written unless the transcript path is subsequently read

#### Scenario: A deep-recall request builds the transcript on demand
- **WHEN** the agent reads the transcript path after a compaction
- **THEN** the transcript file is built from the append-only DB for the covered range and returned

### Requirement: Summarization quality is measured by probes
The system SHALL include a probe harness that runs summarization over captured real agent traces and
scores recall (file paths, decisions, unresolved bugs, skill uses, identifiers preserved) and
precision (redundant tool bodies and reasoning dropped), across the six evaluation dimensions. The
harness SHALL be usable to tune the summarization prompt and clearing policy and to detect quality
drift across re-compaction cycles.

#### Scenario: A recall probe detects a dropped identifier
- **WHEN** a summary omits a file path that was present in the compacted range
- **THEN** the recall probe flags the omission

#### Scenario: A precision probe flags retained bulk
- **WHEN** a summary reproduces verbatim a tool-result body that should have been condensed
- **THEN** the precision probe flags the redundancy
