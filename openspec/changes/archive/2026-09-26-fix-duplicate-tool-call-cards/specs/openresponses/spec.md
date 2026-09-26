# Spec Delta

## MODIFIED Requirements

### Requirement: Server-side tool trace
Tool calls SHALL execute server-side per the agent's allowlist; clients SHALL NOT feed tool outputs back. For each tool call the stream/response SHALL contain exactly one `function_call` output item carrying the call's arguments at `output_item.added`, followed by exactly one `output_item.done` for that same item carrying the same call's id and its complete arguments, then an `onclaw.function_call_output` output item delivered through the standard `response.output_item.added` / `response.output_item.done` events and present in the aggregated `response.output`, carrying the call ID, tool name, result payload, `latency_ms` when the call's duration was measured, and `is_error: true` when the call failed. Items SHALL be paired per call id: a call id already announced SHALL NOT be announced again, a `done` event SHALL belong to the call whose execution finished (never to a later-started call), and no `done` SHALL be dropped when multiple calls run in the same turn. Request-level `tools` SHALL be intersected with the agent's allowlist and SHALL never extend it; `tool_choice: "none"` SHALL run the turn without tools.

#### Scenario: Tool call trace
- **WHEN** an agent executes `web.fetch` during a turn
- **THEN** the output contains a `function_call` item with the call's arguments followed by an `onclaw.function_call_output` item with the fetched result, its measured latency, and the standard output-item added/done events

#### Scenario: Failed tool call trace
- **WHEN** a tool call fails during a turn
- **THEN** its `onclaw.function_call_output` item carries the error payload with `is_error: true`

#### Scenario: Parallel calls each get their own done
- **WHEN** an agent executes two tool calls issued by the same assistant message
- **THEN** the stream contains exactly one `function_call` `output_item.added` and exactly one `output_item.done` per call id, and each `done` carries its own call's id and complete arguments

#### Scenario: Re-announced call id is not duplicated
- **WHEN** the runtime reports a call's start twice for the same call id within one turn
- **THEN** the stream contains a single `function_call` `output_item.added` for that call id

#### Scenario: Request tools cannot extend the allowlist
- **WHEN** a request lists a tool the agent's allowlist excludes
- **THEN** the tool is not exposed for the turn

#### Scenario: Tool choice none
- **WHEN** a request sets `tool_choice: "none"`
