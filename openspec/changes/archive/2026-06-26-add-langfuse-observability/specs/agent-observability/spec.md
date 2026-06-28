# agent-observability

## ADDED Requirements

### Requirement: Agent tracing is opt-in

The system SHALL enable Langfuse tracing for `onclaw run` only when `langfuse.host`,
`langfuse.public_key`, and `langfuse.secret_key` are all configured (config file or
`ONCLAW_LANGFUSE_*` environment variables). When none of the three are set, tracing
SHALL be a no-op and SHALL NOT cause an error. When some but not all are set, the system
SHALL fail before the agent runs, with an error naming the missing field(s). Tracing
SHALL NOT be enabled by default.

#### Scenario: No Langfuse configuration means no tracing

- **WHEN** a user runs `onclaw run` with no `langfuse.*` config or env vars set
- **THEN** the run proceeds normally with no tracing and no error

#### Scenario: Partial configuration fails fast

- **WHEN** `langfuse.host` and `langfuse.public_key` are set but `langfuse.secret_key` is missing
- **THEN** the command fails before the agent runs, with an error naming `secret_key` as the missing field

### Requirement: Agent execution is traced to Langfuse

When enabled, the system SHALL register a Langfuse callback handler
(`github.com/cloudwego/eino-ext/callbacks/langfuse`) on the eino callback bus such that the
model calls, tool calls, and multi-turn agent loop of the running agent are traced to the
configured Langfuse host. A single globally-registered handler SHALL cover the whole agent
execution tree — no per-component wiring (cross-ref `agent-core`).

#### Scenario: A traced turn produces model and tool spans

- **WHEN** a traced `onclaw run` completes a turn that calls a tool and returns an answer
- **THEN** the configured Langfuse project contains a trace with spans for the model invocations and the tool call(s) of that turn

### Requirement: Secrets are masked before external egress

By default, the system SHALL mask known secret patterns in model inputs and outputs before
they are sent to Langfuse, reusing the same redaction applied to transcripts and tools
(`tools.Redact`; cross-ref `agent-tools`, `providers`). The user MAY disable masking by
setting `langfuse.mask: false` (default `true`).

#### Scenario: A secret in a prompt is masked

- **WHEN** a prompt contains an `sk-...` credential token and `langfuse.mask` is at its default
- **THEN** the Langfuse trace shows that token masked rather than in cleartext

#### Scenario: Masking can be disabled

- **WHEN** `langfuse.mask` is set to `false`
- **THEN** model inputs and outputs are sent to Langfuse unmasked

### Requirement: Trace events flush before exit

When tracing is enabled, the system SHALL flush buffered trace events to Langfuse as part of
run teardown, before the process exits and before lower-level resources (e.g. the database
connection) are closed.

#### Scenario: A normal traced run delivers its events

- **WHEN** a traced `onclaw run` ends normally
- **THEN** the events for that turn are flushed before the process exits

### Requirement: Langfuse credentials are never disclosed

The system SHALL NOT write `langfuse.secret_key` to logs and SHALL NOT display its value in
`onclaw config show` — it SHALL be redacted the same way provider API keys are (cross-ref
`providers`).

#### Scenario: config show redacts the secret key

- **WHEN** a user runs `onclaw config show` with `langfuse.secret_key` configured
- **THEN** the output shows `langfuse.secret_key` redacted, never its real value
