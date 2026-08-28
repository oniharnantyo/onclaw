# web-app/chat-runtime Delta — adopt-assistant-ui-chat

## Purpose

A chat runtime bridge that owns conversation lifecycle — send, streaming, running state, cancel, edit, reload, and branch navigation — as a single integration layer between the conversation store and the chat UI, keeping pixels in project components and transport behind handlers.

## ADDED Requirements

### Requirement: Runtime bridge owns conversation lifecycle
The chat SHALL mount a runtime bridge that sources messages from the conversation store and exposes conversation actions as runtime operations. Sending, editing, regenerating, and canceling SHALL all flow through the bridge's handlers; the UI SHALL NOT implement turn logic directly.

#### Scenario: Send flows through bridge
- **WHEN** the user sends a message in an agent chat
- **THEN** the message appends via the bridge and the agent reply is produced by the bridge's response engine, not by component-level logic

#### Scenario: Channel mention fan-out
- **WHEN** a channel message mentions multiple agents
- **THEN** each mentioned agent responds in its own message, staggered in time, via the bridge's response engine

#### Scenario: Teammate DM has no agent turn
- **WHEN** the user sends a message in a teammate DM
- **THEN** no agent turn is scheduled and the running indicator stays idle

### Requirement: Metadata round-trip
Agent identity, cron-origin schedule name, and tool invocation data SHALL survive message conversion unchanged — the transcript SHALL render the same agent name, avatar, cron marker, and tool cards after migration as before.

#### Scenario: Agent identity survives conversion
- **WHEN** an agent message authored by "Atlas" is rendered through the runtime
- **THEN** the transcript shows Atlas's name and avatar exactly as the pre-migration transcript did

#### Scenario: Cron marker survives conversion
- **WHEN** a message produced by schedule "morning-digest" converts through the bridge
- **THEN** the message renders its cron-origin marker naming that schedule

### Requirement: Streaming response lifecycle
While an agent turn is in flight the bridge SHALL expose a running state that drives the thinking indicator, and SHALL stream reply text incrementally into the transcript. The send control SHALL become a stop control that cancels the in-flight turn, leaving any partial text in the transcript.

#### Scenario: Incremental arrival
- **WHEN** an agent reply is being produced
- **THEN** the reply text appears progressively in the transcript (not as one whole message followed by a caret)

#### Scenario: Streaming caret removal
- **WHEN** an agent reply completes
- **THEN** no post-completion caret animation plays on the finished message

#### Scenario: Stop control cancels
- **WHEN** the user presses stop during an in-flight turn
- **THEN** the turn ends, running state clears, and the partial text stays in the transcript

### Requirement: Tool-card lifecycle
An agent tool invocation SHALL render as a card whose state follows the turn lifecycle: running while its turn is in flight, completed with latency once the turn's tool phase resolves.

#### Scenario: Running card completes with latency
- **WHEN** an agent message with a tool invocation finishes its turn
- **THEN** the card shows running during the turn, then latency (e.g. `760ms`) on completion

### Requirement: Behavior scoping to agent DMs
Edit, reload, and branch affordances SHALL apply to agent direct chats only. Channel agent messages SHALL NOT offer regenerate, and channel transcripts SHALL render exactly one authoritative variant per agent message.

#### Scenario: No regenerate in channels
- **WHEN** a channel agent message renders
- **THEN** the regenerate control is absent from that message's hover controls

#### Scenario: Affordances present in agent DMs
- **WHEN** an agent DM message renders
- **THEN** copy and reload controls are available as today (or runtime equivalents)

### Requirement: Visual parity constraint
Adopting the runtime SHALL NOT change the rendered appearance of the chat: fonts, spacing, states, and interactions reproduce `web/Web-Prototype/` as pinned by the parity suite, except where a delta in this change explicitly changes behavior.

#### Scenario: Parity suite green
- **WHEN** the parity suite runs after migration
- **THEN** all parity tests pass without baseline rewrites attributable to the runtime adoption
