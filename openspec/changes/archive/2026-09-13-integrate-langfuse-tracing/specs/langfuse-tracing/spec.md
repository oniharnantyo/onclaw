## Purpose

Optional turn-level observability: when a self-hosted Langfuse backend is configured, every agent turn — web, API, scheduler, or Telegram origin — exports to Langfuse as one trace carrying the turn's model calls, tool calls, and attribution, with secrets masked and export cost controlled by sampling.

## ADDED Requirements

### Requirement: Tracing is configuration-gated
Trace export SHALL be enabled only when a Langfuse host and API keys are provided in server configuration. With no configuration, the system SHALL behave exactly as before: no export, no new runtime dependencies loaded, no trace ids persisted. Enabling or disabling tracing SHALL require only a server configuration change, not schema or code changes.

#### Scenario: Unconfigured instance is unchanged
- **WHEN** the server starts with no Langfuse configuration and an agent turn runs
- **THEN** no trace is exported, no trace id is recorded, and behavior is identical to a pre-tracing deployment

#### Scenario: Configured instance exports
- **WHEN** the server starts with a Langfuse host and keys and an agent turn runs
- **THEN** a trace for that turn is exported to the configured backend

### Requirement: Trace-per-turn mapping
Each agent turn SHALL export as exactly one Langfuse trace. Model invocations within the turn SHALL export as generations (streaming handled) and tool invocations as spans. A retried turn's attempts SHALL be represented within the turn's trace, not as separate traces.

#### Scenario: One execution, one trace
- **WHEN** an agent turn executes with two model calls and three tool calls
- **THEN** Langfuse records one trace containing two generations and three spans

#### Scenario: Retry stays inside the trace
- **WHEN** a turn's model call fails once and is retried successfully
- **THEN** the trace contains both attempts and completes successfully

### Requirement: Turn attribution on traces
Every exported trace SHALL carry: the session id as the Langfuse session (so a session's turns group longitudinally), the acting member's user id, and tags/metadata identifying the workspace, agent, and run origin (web, API, scheduler, or gateway). A trace for a turn executed under a member SHALL be attributable to that member; scheduled and gateway turns SHALL be filterable by origin.

#### Scenario: Session grouping in Langfuse
- **WHEN** several turns run on the same session with tracing enabled
- **THEN** Langfuse groups all resulting traces under that session

#### Scenario: Origin and workspace filterable
- **WHEN** an operator filters Langfuse traces by the gateway origin tag or workspace metadata
- **THEN** only turns from that origin or workspace match

### Requirement: Secret masking before export
Exported trace content SHALL have secrets redacted before leaving the instance: tool configuration secrets (API keys and credential-shaped values) and hook secret payloads SHALL be replaced with a mask, in both model and tool span content. The masking function SHALL be applied to every export, not opt-in per trace.

#### Scenario: Configured tool secret never leaves
- **WHEN** a traced tool call's arguments or results contain a configured workspace credential
- **THEN** the exported span carries a masked placeholder instead of the secret value

### Requirement: Export cost controls
Trace export SHALL be batched and asynchronous so that export never blocks or slows turn execution. A configurable sampling rate SHALL bound export volume (1.0 = all turns; fractions sample deterministically per run). Export failures SHALL be retried with a bounded attempt count and SHALL NOT affect run success or the user-visible transcript.

#### Scenario: Export never blocks the turn
- **WHEN** the Langfuse backend is slow or unreachable
- **THEN** the turn completes normally and its user-visible transcript is unaffected

#### Scenario: Sampling bounds volume
- **WHEN** the sample rate is 0.1 and 100 turns run
- **THEN** approximately 10 traces are exported, chosen deterministically

### Requirement: Trace id persistence and link-out
When tracing is enabled, the exported trace's id SHALL be persisted with the run so product surfaces can deep-link to it. A run view SHALL render an "Open in Langfuse" action only when the run carries a trace id; runs without one (tracing disabled, sampled out, or pre-dating enablement) SHALL render no such action. The link SHALL target the configured Langfuse host.

#### Scenario: Traced run deep-links
- **WHEN** a run with a persisted trace id is viewed in the runs surface
- **THEN** an "Open in Langfuse" action is offered, targeting the trace on the configured host

#### Scenario: Untraced run unchanged
- **WHEN** a run without a trace id is viewed
- **THEN** no Langfuse action is rendered
