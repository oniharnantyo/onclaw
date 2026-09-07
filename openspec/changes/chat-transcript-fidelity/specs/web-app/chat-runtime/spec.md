## MODIFIED Requirements

### Requirement: Streaming response lifecycle
While an agent turn is in flight the bridge SHALL expose a running state that drives the thinking indicator, and SHALL stream reply text incrementally into the transcript. Reasoning deltas from the stream SHALL accumulate onto the in-flight agent message as reasoning content kept separate from reply text. The send control SHALL become a stop control that cancels the in-flight turn, leaving any partial text in the transcript.

#### Scenario: Incremental arrival
- **WHEN** an agent reply is being produced
- **THEN** the reply text appears progressively in the transcript (not as one whole message followed by a caret)

#### Scenario: Reasoning accumulates separately
- **WHEN** the stream delivers reasoning deltas followed by text deltas
- **THEN** the in-flight message carries the reasoning content and the reply text as distinct fields, and neither leaks into the other

#### Scenario: Streaming caret removal
- **WHEN** an agent reply completes
- **THEN** no post-completion caret animation plays on the finished message

#### Scenario: Stop control cancels
- **WHEN** the user presses stop during an in-flight turn
- **THEN** the turn ends, running state clears, and the partial text stays in the transcript

### Requirement: Tool-card lifecycle
An agent tool invocation SHALL render as a card whose state follows the call lifecycle: running while the call is in flight, completed with its measured latency once the call's output event arrives. The bridge SHALL capture the call's arguments from the function-call stream item and attach the result, error flag, and latency from the tool output event to that same card. Hydrated cards SHALL carry the same fields projected by the server transcript. The bridge SHALL NOT synthesize placeholder arguments, results, or latencies for any card.

#### Scenario: Running card completes with latency
- **WHEN** an agent message's tool call starts and its output event arrives
- **THEN** the card shows running during the call, then the measured latency (e.g. `760ms`) on completion

#### Scenario: Arguments captured from the stream
- **WHEN** a function-call item with arguments arrives during a live turn
- **THEN** the card carries those arguments rather than an empty string

#### Scenario: Output lands on the right card
- **WHEN** a tool output event arrives for a call id during a live turn
- **THEN** the result attaches to the card opened for that call id, and an errored output marks the card as failed

#### Scenario: Hydrated cards keep fidelity
- **WHEN** a transcript is hydrated from the server for a session with tool calls
- **THEN** the rendered cards carry arguments, results, error flags, and latency from the server transcript

### Requirement: Transcript hydration
Opening an agent chat whose live session is bound SHALL load the server-side transcript for that session and render it in the thread, so transcripts converge across browsers and reloads. Hydrated history SHALL render through the same message components as local history — including markdown, reasoning sections, and tool-card fidelity fields — and SHALL NOT duplicate or displace messages already present locally.

#### Scenario: Second browser sees history
- **WHEN** the user opens a bound chat from a browser that has never seen it
- **THEN** the thread renders the server-side transcript of the bound session

#### Scenario: Local messages not duplicated
- **WHEN** a chat holds local messages that also exist in the hydrated server transcript
- **THEN** the thread renders them once

## ADDED Requirements

### Requirement: Turn failure surfacing
When a live turn ends in a terminal error from the stream, the bridge SHALL append an error entry to the active thread carrying the server error message and SHALL clear the running state. A turn that failed before producing any content SHALL retract its empty optimistic agent message so no permanent loading placeholder remains. Authentication failures SHALL follow the key re-exchange path and surface the connect state instead of an error entry; a retried turn after re-exchange SHALL stream into the original optimistic message rather than duplicating it.

#### Scenario: Terminal error appends an entry
- **WHEN** a live turn ends with `response.failed` carrying an error message
- **THEN** an error entry with that message is appended to the thread and the running state clears

#### Scenario: Empty optimistic row retracted
- **WHEN** a live turn fails before streaming any text or tool card
- **THEN** the empty optimistic agent message is removed from the thread and the error entry represents the turn

#### Scenario: Auth failure takes the connect path
- **WHEN** a live turn fails with an authentication error and key re-exchange also fails
- **THEN** the connect state with retry renders and no error entry is appended
