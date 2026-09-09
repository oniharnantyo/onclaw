## ADDED Requirements

### Requirement: Compact command
A request MAY set `metadata.onclaw_command` to `compact` to compact the bound session's context instead of running a normal model turn. The command SHALL require a session binding that resolves to persisted events in the key's workspace: an unknown or foreign `metadata.onclaw_session` SHALL fail not-found indistinguishable from a foreign workspace's session and SHALL NEVER birth a session; `previous_response_id` binds unchanged. The request's `input`, when non-empty, SHALL steer the summary generation as focus text and SHALL NOT be persisted as a user message. The turn SHALL stream `onclaw:context_compacted` (see Streaming event contract) when compaction occurs and SHALL terminate with `response.completed` carrying the summarizer call's usage and no output items; a bound session with no compactable history SHALL complete without the compaction event. Compact requests SHALL be subject to the same single-active-run conflict behavior as ordinary turns.

#### Scenario: Compact turn stream
- **WHEN** a compact-command request executes against a session with history
- **THEN** the stream contains `onclaw:context_compacted` carrying the token estimates, then `response.completed` with the summarizer usage and no output items, then [DONE]

#### Scenario: Focus text steers without persisting
- **WHEN** a compact request carries `input` "keep the API design decisions"
- **THEN** the summary is generated under that focus instruction and no user message is appended to the session

#### Scenario: Compact never births a session
- **WHEN** a compact request carries `metadata.onclaw_session` naming a session with no persisted events in the key's workspace
- **THEN** the request fails not-found and no session is created

#### Scenario: Quiet completion on empty history
- **WHEN** a compact request binds a session that exists but has no compactable message history
- **THEN** the response completes normally without emitting `onclaw:context_compacted`

#### Scenario: Conflict with an active run
- **WHEN** a compact request is submitted while another run is active on the same session
- **THEN** the same conflict behavior applies as for an ordinary turn

## MODIFIED Requirements

### Requirement: Streaming event contract
With `stream: true`, the endpoint SHALL emit `text/event-stream` with `data: <json>` frames, a terminal `data: [DONE]` line, and a monotonic integer `sequence_number` on every event. The stream SHALL follow the OpenResponses lifecycle: `response.created`, `response.in_progress`, per-item `output_item.added` / `output_item.done` with content-part and text-delta events bracketing assistant text, and exactly one terminal response event (`response.completed`, `response.failed`, or `response.incomplete`). Reasoning the model produces during generation SHALL stream as custom `onclaw:reasoning_delta` events, each carrying one incremental chunk of reasoning text, interleaved with text deltas and always before the terminal event. When a turn compacts the session's context — manually or by threshold — the stream SHALL emit a custom `onclaw:context_compacted` event carrying the working-context token estimates before and after compaction, before the terminal event. The same event SHALL be delivered by the catch-up attach replay of an in-flight run.

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

#### Scenario: Compaction event on the stream
- **WHEN** a turn compacts the session context, manually or at threshold
- **THEN** `onclaw:context_compacted` carrying the before/after token estimates arrives before the terminal event, and a client that ignores unknown event types still receives a valid lifecycle
