## MODIFIED Requirements

### Requirement: Streaming event contract
With `stream: true`, the endpoint SHALL emit `text/event-stream` with `data: <json>` frames, a terminal `data: [DONE]` line, and a monotonic integer `sequence_number` on every event. The stream SHALL follow the OpenResponses lifecycle: `response.created`, `response.in_progress`, per-item `output_item.added` / `output_item.done` with content-part and text-delta events bracketing assistant text, and exactly one terminal response event (`response.completed`, `response.failed`, or `response.incomplete`). Reasoning the model produces during generation SHALL stream as custom `onclaw:reasoning_delta` events, each carrying one incremental chunk of reasoning text, interleaved with text deltas and always before the terminal event.

#### Scenario: Text turn stream
- **WHEN** an agent answers with text only
- **THEN** the stream contains created → in_progress → message item added → content part added → text deltas → part done → item done → completed → [DONE], with strictly increasing sequence numbers

#### Scenario: Reasoning turn stream
- **WHEN** the model produces reasoning before or during its answer
- **THEN** `onclaw:reasoning_delta` events carrying the reasoning chunks are interleaved with the stream before the terminal event, and a client that ignores unknown event types still receives a valid lifecycle

#### Scenario: Tool turn stream
- **WHEN** an agent invokes a tool
- **THEN** the stream emits the `function_call` item (added, then done with arguments) followed by the tool trace item (see "Server-side tool trace"), then continues

#### Scenario: Failed turn
- **WHEN** the run ends with the error transcript event
- **THEN** the stream terminates with `response.failed` carrying the error, then [DONE]

### Requirement: Server-side tool trace
Tool calls SHALL execute server-side per the agent's allowlist; clients SHALL NOT feed tool outputs back. For each tool call the stream/response SHALL contain a `function_call` output item carrying the call's arguments at `output_item.added`, followed by an `onclaw.function_call_output` output item delivered through the standard `response.output_item.added` / `response.output_item.done` events and present in the aggregated `response.output`, carrying the call ID, tool name, result payload, `latency_ms` when the call's duration was measured, and `is_error: true` when the call failed. Request-level `tools` SHALL be intersected with the agent's allowlist and SHALL never extend it; `tool_choice: "none"` SHALL run the turn without tools.

#### Scenario: Tool call trace
- **WHEN** an agent executes `web.fetch` during a turn
- **THEN** the output contains a `function_call` item with the call's arguments followed by an `onclaw.function_call_output` item with the fetched result, its measured latency, and the standard output-item added/done events

#### Scenario: Failed tool call trace
- **WHEN** a tool call fails during a turn
- **THEN** its `onclaw.function_call_output` item carries the error payload with `is_error: true`

#### Scenario: Request tools cannot extend the allowlist
- **WHEN** a request lists a tool the agent's allowlist excludes
- **THEN** the tool is not exposed for the turn

#### Scenario: Tool choice none
- **WHEN** a request sets `tool_choice: "none"`
- **THEN** the turn runs with no tools and the model cannot invoke any
