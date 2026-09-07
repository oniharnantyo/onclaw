# web-app/chat-runtime Specification

# web-app/chat-runtime Delta — adopt-assistant-ui-chat

## Purpose

A chat runtime bridge that owns conversation lifecycle — send, streaming, running state, cancel, edit, reload, and branch navigation — as a single integration layer between the conversation store and the chat UI, keeping pixels in project components and transport behind handlers.

## Requirements

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

### Requirement: Live session binding
Live agent turns SHALL bind to persistent server sessions following the OpenResponses convention: the first live turn of a chat session SHALL carry a client-minted `sess_<uuid>` as `metadata.onclaw_session` (birthing the server-side session), and every subsequent turn of that chat session SHALL chain via `previous_response_id` from the previous turn's minted response ID. The runtime SHALL record each assistant turn's response ID on the message it produced. `/reset` SHALL start the next live turn on a freshly minted session ID.

#### Scenario: Birth turn creates server memory
- **WHEN** the user sends the first live message in a chat session
- **THEN** the turn carries a fresh session ID as metadata and the reply remembers it on the next turn

#### Scenario: Continuation chains
- **WHEN** the user sends a second message in the same chat session
- **THEN** the turn carries the previous turn's response ID and the agent answers with full earlier context

#### Scenario: Reset forks the session
- **WHEN** the user runs `/reset` in a live chat and sends a new message
- **THEN** the turn births a new session and the agent has no memory of the cleared conversation

#### Scenario: Reload keeps the thread
- **WHEN** the user reloads the page mid-conversation and sends another message
- **THEN** the turn chains from the recorded response ID and the conversation continues with context

### Requirement: Live cancel
The stop control during a live turn SHALL cancel the server-side run: the runtime SHALL address the native session-scoped cancel endpoint using the in-flight turn's minted response identity captured from the stream, and the run SHALL stop producing events. Cancelling SHALL leave partial text and any completed tool cards in the transcript.

#### Scenario: Stop stops the server run
- **WHEN** the user presses stop while a live turn is streaming
- **THEN** the native cancel endpoint is called for the in-flight turn and no further reply text arrives after the stream ends

#### Scenario: Partial work preserved
- **WHEN** a live turn is cancelled after partial text and a completed tool call
- **THEN** the partial text and the tool card remain in the transcript

### Requirement: Transcript hydration
Opening an agent chat whose live session is bound SHALL load the server-side transcript for that session and render it in the thread, so transcripts converge across browsers and reloads. Hydrated history SHALL render through the same message components as local history, and SHALL NOT duplicate or displace messages already present locally.

#### Scenario: Second browser sees history
- **WHEN** the user opens a bound chat from a browser that has never seen it
- **THEN** the thread renders the server-side transcript of the bound session

#### Scenario: Local messages not duplicated
- **WHEN** a chat holds local messages that also exist in the hydrated server transcript
- **THEN** the thread renders them once

### Requirement: Live regenerate
Regenerating an agent message in a live chat SHALL re-run the turn through the live bridge so the new variant is a real agent completion chained to the conversation's session, replacing the canned-variant fallback. Variant mechanics (branch state, `n / total` picker, agent-DM scoping) are unchanged.

#### Scenario: Regenerate produces a real variant
- **WHEN** the user regenerates the latest live reply in an agent DM
- **THEN** a new variant produced by a real turn appears and the picker shows the incremented count

### Requirement: Chat key provisioning
The app SHALL provision the workspace chat key automatically: on entry to a workspace, if no chat key for that workspace is held, the app SHALL call the native session key exchange endpoint and store the returned key per workspace. With no usable chat key, live chat SHALL present an explicit connect state with a retry path; the app SHALL NEVER fall back to canned simulated agent replies in the live UI. Keys SHALL be dropped on logout.

#### Scenario: Automatic provisioning
- **WHEN** a member enters a workspace for the first time
- **THEN** a workspace-scoped chat key is obtained via the exchange endpoint and used for live turns without any manual paste

#### Scenario: Per-workspace scoping
- **WHEN** the user switches from workspace A to workspace B and chats
- **THEN** the turn authenticates with workspace B's chat key, not workspace A's

#### Scenario: Connect state instead of fake replies
- **WHEN** key provisioning fails and the user sends a message to an agent
- **THEN** the UI shows an explicit connect/retry state and no simulated reply is produced

#### Scenario: Logout clears keys
- **WHEN** the user logs out
- **THEN** held chat keys are removed from the browser
