## MODIFIED Requirements

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

## ADDED Requirements

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