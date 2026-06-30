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

The agent run SHALL emit model tokens progressively as a stream of `*schema.AgenticMessage`
values and SHALL NOT buffer the full response before emitting. The CLI SHALL render that stream
to stdout as the tokens arrive. The agent core SHALL perform no presentation I/O itself;
rendering is the caller's responsibility (cross-ref the headless-output requirement).

#### Scenario: Output appears incrementally

- **WHEN** the model emits streamed content
- **THEN** tokens are rendered to stdout progressively, not deferred to end-of-turn

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

