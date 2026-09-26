# Spec Delta

## MODIFIED Requirements

### Requirement: Tool-card lifecycle
An agent tool invocation SHALL render as a card whose state follows the call lifecycle: running while the call is in flight, completed with its measured latency once the call's output event arrives. A card's identity is its call id: the bridge SHALL open at most one card per call id for the life of a turn — a function-call or tool-output event referencing a call id that already has a card SHALL update that card (attaching arguments when the event carries them) and SHALL NOT mint a second card. The bridge SHALL capture the call's arguments from the function-call stream item and attach the result, error flag, and latency from the tool output event to that same card. Message conversion SHALL emit at most one tool-call part per `toolCallId` within a message, so duplicate card entries can never produce duplicate resource keys. Hydrated cards SHALL carry the same fields projected by the server transcript. The bridge SHALL NOT synthesize placeholder arguments, results, or latencies for any card.

#### Scenario: Running card completes with latency
- **WHEN** an agent message's tool call starts and its output event arrives
- **THEN** the card shows running during the call, then the measured latency (e.g. `760ms`) on completion

#### Scenario: Arguments captured from the stream
- **WHEN** a function-call item with arguments arrives during a live turn
- **THEN** the card carries those arguments rather than an empty string

#### Scenario: Output lands on the right card
- **WHEN** a tool output event arrives for a call id during a live turn
- **THEN** the result attaches to the card opened for that call id, and an errored output marks the card as failed

#### Scenario: One card per call id
- **WHEN** a turn's stream delivers more than one function-call event carrying the same call id, or a function-call event with empty arguments for a call id that already has a card
- **THEN** exactly one card exists for that call id when the turn ends, and the transcript renders without a duplicate tool-call key error

#### Scenario: Hydrated cards keep fidelity
- **WHEN** a transcript is hydrated from the server for a session with tool calls
- **THEN** the rendered cards carry arguments, results, error flags, and latency from the server transcript
