# web-app/chat-runtime delta

## MODIFIED Requirements

### Requirement: Runtime bridge owns conversation lifecycle
The chat SHALL mount a runtime bridge that sources messages from the conversation store and exposes conversation actions as runtime operations. Sending, editing, regenerating, and canceling SHALL all flow through the bridge's handlers; the UI SHALL NOT implement turn logic directly. In channels, sending posts to the feed API and agent replies arrive as live channel events through the bridge — the client-side mention fan-out engine SHALL no longer drive channel conversations.

#### Scenario: Send flows through bridge
- **WHEN** the user sends a message in an agent chat
- **THEN** the message appends via the bridge and the agent reply is produced by the bridge's response engine, not by component-level logic

#### Scenario: Channel message flows through the bridge
- **WHEN** the user sends a message in a channel
- **THEN** the bridge posts it to the feed API and agent replies arrive from the channel event stream as real run output

#### Scenario: Channel mention fan-out
- **WHEN** a channel message mentions multiple agents
- **THEN** each mentioned agent is summoned through the live backend and responds in its own message via its own run

#### Scenario: Teammate DM has no agent turn
- **WHEN** the user sends a message in a teammate DM
- **THEN** no agent turn is scheduled and the running indicator stays idle

## ADDED Requirements

### Requirement: Channel live transport
The channel room SHALL subscribe to the channel event stream after loading the feed via REST, keyed by the sequence cursor so live events deduplicate against the loaded history. An agent message whose run is still streaming SHALL attach to the run's live stream using the existing re-attachment machinery; a disconnect SHALL reconnect and recover missed feed events from the REST cursor without duplicating or losing messages.

#### Scenario: Live reply from a summoned agent
- **WHEN** another member's mention summons an agent while the room is open
- **THEN** the considering affordance, the streamed reply, and the run-finished state all arrive through the channel stream and run attachment

#### Scenario: Reconnect recovers the gap
- **WHEN** the stream drops and reconnects after two messages were posted
- **THEN** the room fetches the gap from the feed cursor and renders both exactly once
