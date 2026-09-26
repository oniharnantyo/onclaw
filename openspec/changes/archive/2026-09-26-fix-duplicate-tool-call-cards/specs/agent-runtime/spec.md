# Spec Delta

## MODIFIED Requirements

### Requirement: Streaming execution
An agent execution SHALL stream transcript events to its caller as they occur and SHALL end with exactly one terminal event (completed, error, or cancelled). Assistant text and reasoning SHALL be delivered as incremental delta events during generation; deltas SHALL NOT be persisted. Each completed assistant message SHALL be persisted exactly once. An execution interrupted mid-generation SHALL persist an incomplete-message marker so a later reload renders the partial response from durable data alone. Each executed tool call SHALL be announced on the live stream by exactly one `tool_call_started` event carrying that call's id, name, and arguments, followed by exactly one `tool_call_started`-paired `tool_call_finished` event carrying the result, error flag, and measured latency — no matter which internal event source (message blocks or execution spans) surfaces the call, and no matter how many calls the model issued in parallel. A call id SHALL NOT be started twice on one execution's live stream. The live events SHALL agree field-for-field with the hydrated transcript projection of the same execution, including each started event's arguments.

#### Scenario: Deltas stream during generation
- **WHEN** an agent generates a response
- **THEN** the caller receives incremental text/reasoning delta events before the completed message event

#### Scenario: Interrupted generation reloads as partial
- **WHEN** an execution is cancelled or fails mid-stream after partial text was emitted
- **THEN** the persisted history contains an incomplete-message marker and the emitted partial content, and a reload shows the partial response

#### Scenario: Terminal event exactly once
- **WHEN** an execution finishes (any outcome)
- **THEN** the stream ends with exactly one terminal event and no events follow it

#### Scenario: Tool call announced once with arguments
- **WHEN** an execution executes a tool call whose arguments the model emitted in the requesting assistant message, on a provider whose live message stream does not surface tool-call blocks (span-sourced call)
- **THEN** the live stream contains exactly one `tool_call_started` event for that call id, carrying the same call's arguments as recorded in the persisted transcript, followed by exactly one matching `tool_call_finished`

#### Scenario: Parallel tool calls each paired
- **WHEN** an execution executes two or more tool calls issued by the same assistant message
- **THEN** the live stream contains exactly one started event and one finished event per call id, and no finished event is attributed to a different call's id

#### Scenario: Live and hydrated transcripts agree
- **WHEN** the same executed tool call is observed on the live stream and then replayed from the session log
- **THEN** both projections carry the same call id, name, arguments, result, error flag, and latency
