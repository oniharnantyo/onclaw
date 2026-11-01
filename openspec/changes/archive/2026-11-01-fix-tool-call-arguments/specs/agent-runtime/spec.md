## MODIFIED Requirements

### Requirement: Streamed turns

A turn's output SHALL be delivered incrementally to the UI as it is produced — assistant text deltas, reasoning, and tool activity — rather than appearing only at completion. The final transcript MUST be persisted regardless of whether the client stays connected.

Each tool activity's terminal record SHALL carry the call's wall-clock duration, measured from the moment the call was announced as running to the moment its result landed, so a reloaded transcript shows the same latency the live view did. Records persisted before durations were recorded carry none, and the runtime MUST NOT fabricate one for them.

Each tool activity's terminal record SHALL also carry the call's arguments exactly as the model delivered them, for every provider family and regardless of how the provider chunks a call across stream frames, so a reloaded transcript shows the same request the live view did. Records persisted before arguments were captured carry none, and the runtime MUST NOT fabricate ones for them.

#### Scenario: Token-by-token rendering

- **WHEN** an agent is producing a response
- **THEN** the chat view renders partial output as it arrives

#### Scenario: Disconnected client does not lose the turn

- **WHEN** the web client disconnects mid-turn and reconnects after completion
- **THEN** the completed turn is present in the session transcript

#### Scenario: Tool duration persists

- **WHEN** a tool call completes and the session is later reloaded
- **THEN** the call's terminal record carries the duration observed by the runtime and the reloaded card shows the same latency as the live card did

#### Scenario: Tool arguments persist across providers

- **WHEN** a tool call completes on any provider family and the session is later reloaded
- **THEN** the call's terminal record carries the arguments the model delivered and the reloaded card shows the call's request (for a shell call, its command) beside the result

#### Scenario: Chunked call frames keep one record

- **WHEN** a provider streams one tool call as several frames whose argument text arrives on frames that identify the call incompletely
- **THEN** the frames merge into that call's single record with the full argument text, and no orphan record is persisted for the unidentifiable frames
