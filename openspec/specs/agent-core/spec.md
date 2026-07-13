# agent-core

## Purpose

Run a real, streaming, tool-calling ReAct agent loop over a remote LLM within a bounded context, with clean cancellation and durable multi-turn memory (cross-ref `conversation-history`).
## Requirements
### Requirement: The agent runs a tool-calling ReAct loop over a remote ChatModel

The system SHALL run the agent as an eino ADK `TypedChatModelAgent[*schema.AgenticMessage]`
that reasons, invokes tools, and produces an answer from an agentic ChatModel
(`model.AgenticModel`, i.e. `BaseModel[*schema.AgenticMessage]`) built from the effective
provider profile resolved from the selected agent (cross-ref `agent-profiles`). Each provider
type SHALL be backed by its dedicated eino-ext agentic model package (`agenticopenai`,
`agenticclaude`, `agenticgemini`, `agenticdeepseek`, `agenticqwen`, `agenticark`); `ollama` and
`openai-compatible` SHALL route through the OpenAI agentic model with the profile's `BaseURL`.
`onclaw run` SHALL submit a one-shot prompt and stream the result; `onclaw chat` SHALL run an
interactive read-eval-print loop, one turn per line of input.

#### Scenario: A one-shot prompt streams an answer

- **WHEN** a user runs `onclaw run "summarize README.md"` with a configured provider
- **THEN** the agent reasons, may call tools, and streams a final answer to stdout

#### Scenario: An interactive chat runs one turn per line

- **WHEN** a user runs `onclaw chat` and types a prompt followed by Enter
- **THEN** the agent completes that turn before reading the next line

### Requirement: Assistant tokens stream to the user as they arrive

The agent run SHALL emit model output as a stream of `*schema.AgenticMessage` values whose
granularity is controlled per channel by the run's streaming flag (Eino's
`TypedAgentInput.EnableStreaming`). When streaming is enabled, the model SHALL be invoked via its
streaming path and the run SHALL emit token-level delta chunks, each carrying one content block
stamped with a stable `streaming_meta.index`; the run SHALL NOT buffer a model call's full
response before emitting its deltas. When streaming is disabled, the run SHALL emit one complete
message per model call. The **web** channel SHALL enable streaming so tokens render progressively;
the **CLI** channel SHALL disable streaming and render at message granularity (one rendered
message per model call), consistent with the low-resource CLI target. The agent core SHALL perform
no presentation I/O itself; rendering is the caller's responsibility (cross-ref the headless-output
requirement).

#### Scenario: The web channel streams tokens progressively

- **WHEN** the web channel runs a turn with streaming enabled
- **THEN** the model is invoked via its streaming path and assistant tokens are emitted and rendered progressively, not deferred to the end of each model call

#### Scenario: The CLI channel renders at message granularity

- **WHEN** the CLI runs a turn with streaming disabled
- **THEN** each model call's complete message is rendered before the next begins, and the CLI does not emit or render token-level deltas

### Requirement: The agent stays within the configured context budget

The system SHALL keep the conversation within `max_context_tokens` (default 8192) by
summarizing/compacting history before it exceeds the limit. The summarization SHALL trigger
well below the hard limit (around ~6000 tokens) so a turn never overruns the budget. Compaction
SHALL be durable across runs: the summary and a coverage cursor SHALL be persisted so that
subsequent replays stay bounded (cross-ref `conversation-history`).

#### Scenario: A long conversation is compacted

- **WHEN** accumulated history approaches the trigger threshold
- **THEN** older messages are summarized and recent messages are retained, the summary and coverage cursor are persisted, and the turn completes without exceeding `max_context_tokens`

### Requirement: Cancellation exits cleanly without a torn turn

The system SHALL propagate a cancellation signal (Ctrl-C, `/stop`, or a cancelled context) into
the running turn. On cancellation the loop SHALL stop promptly and return without panicking or
leaving a half-written turn. A partial assistant message produced mid-stream SHALL NOT be
persisted as a complete message. Cancellation is a control-flow condition and SHALL NOT be
recorded as a persisted message row.

#### Scenario: Ctrl-C mid-stream is clean

- **WHEN** the user interrupts a streaming turn
- **THEN** the turn stops and the process exits cleanly with no partial assistant message presented or persisted as complete

### Requirement: The agent run produces a headless stream the caller renders

The system SHALL expose an agent turn as a method on the agent that returns a pull-based stream
of `*schema.AgenticMessage` values and performs no I/O. Errors and turn completion SHALL be
carried by the iterator, not as message kinds. Each frontend (the CLI today; future API/Web)
SHALL consume the stream through its own renderer. Cancellation SHALL propagate into the
running turn via the turn context and SHALL surface as the iterator's error.

#### Scenario: The CLI renders the stream

- **WHEN** the CLI runs a turn
- **THEN** it drains the agent's message stream through a text renderer that writes to stdout

#### Scenario: Cancellation surfaces cleanly

- **WHEN** a turn's context is cancelled mid-stream
- **THEN** the iterator stops and reports the cancellation error, with no partial assistant message presented or persisted as complete (cross-ref `conversation-history`)

### Requirement: The agent handler chain includes a memory middleware

The agent assembly SHALL include a memory middleware in its handler chain alongside
summarization, history, skill, and hooks. The middleware SHALL inject the curated memory core at
session start and SHALL run memory extraction after agent runs. Memory loading SHALL replace the
prior silent truncation of `MEMORY.md` under the shared persona-file budget with an explicit,
self-contained cap.

#### Scenario: The memory middleware is part of the assembled agent

- **WHEN** an agent is assembled
- **THEN** the handler chain includes the memory middleware and `MEMORY.md` is loaded under the memory core's own cap

### Requirement: Per-channel streaming control

The system SHALL control the agent run's streaming mode through a per-call context value that is
read when constructing the run input — a peer of the existing previous-response-id context option.
The web `/api/chat` handler SHALL set streaming enabled; the CLI `run` and `chat` commands SHALL
leave it unset so it defaults to disabled. Streaming SHALL be a transport-layer decision made at
each channel's entry point; no field SHALL be added to the chat request DTO or the service layer
for it.

#### Scenario: The web handler enables streaming

- **WHEN** the web `/api/chat` handler runs a turn
- **THEN** it sets the streaming context value to enabled before invoking the agent run, and the run emits token-level deltas

#### Scenario: The CLI leaves streaming disabled

- **WHEN** the CLI `run` or `chat` command runs a turn
- **THEN** the streaming context value is unset and the run emits one complete message per model call

#### Scenario: The flag defaults to disabled

- **WHEN** a caller invokes the agent run without setting the streaming context value
- **THEN** streaming is disabled and the run behaves as a non-streaming (buffered) run

