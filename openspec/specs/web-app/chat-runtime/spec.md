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
Live agent turns SHALL bind to persistent server sessions following the OpenResponses convention: the first live turn of a chat session SHALL carry a client-minted `sess_<uuid>` as `metadata.onclaw_session` (birthing the server-side session), and every subsequent turn of that chat session SHALL chain via `previous_response_id` from the previous turn's minted response ID. The client SHALL immediately and synchronously persist newly minted session bindings to durable local storage upon message submission before network dispatch, ensuring page reloads retain the server session address. The runtime SHALL record each assistant turn's response ID on the message it produced. `/reset` SHALL start the next live turn on a freshly minted session ID.

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

#### Scenario: Immediate page reload retains session binding
- **WHEN** the user sends a message and reloads the browser within milliseconds
- **THEN** the reloaded application loads the exact same `sess_<uuid>` from local storage and hydrates against the server session
### Requirement: Live cancel
The stop control during a live turn SHALL cancel the server-side run: the runtime SHALL address the native session-scoped cancel endpoint using the in-flight turn's minted response identity captured from the stream, and the run SHALL stop producing events. Cancelling SHALL leave partial text and any completed tool cards in the transcript.

#### Scenario: Stop stops the server run
- **WHEN** the user presses stop while a live turn is streaming
- **THEN** the native cancel endpoint is called for the in-flight turn and no further reply text arrives after the stream ends

#### Scenario: Partial work preserved
- **WHEN** a live turn is cancelled after partial text and a completed tool call
- **THEN** the partial text and the tool card remain in the transcript

### Requirement: Transcript hydration
Opening an agent chat whose live session is bound SHALL load the server-side transcript for that session and render it in the thread, so transcripts converge across browsers and reloads. Hydrated history SHALL render through the same message components as local history — including markdown, reasoning sections, and tool-card fidelity fields — and SHALL NOT duplicate or displace messages already present locally.

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

### Requirement: Active run re-attachment on chat load
When a chat view or thread mounts with an active bound server session (`sess_<uuid>`), the client runtime SHALL check if an in-flight run is active or unfinished. If an execution is in progress, the client SHALL initiate a streaming connection to catch up missed events and attach to the live stream, streaming reasoning deltas, tool cards, and text deltas directly into the active assistant message until the turn completes.

#### Scenario: Page reload mid-turn resumes live stream
- **WHEN** the user refreshes the browser while the agent is executing tools or generating text
- **THEN** the chat re-attaches to the ongoing execution, renders completed tool cards and reasoning parts, and continues streaming new deltas to completion without manual user intervention

#### Scenario: Page reload after turn completed renders full response
- **WHEN** the user refreshes the browser and returns after the agent execution has finished
- **THEN** the hydration catches up all persisted events and renders the complete message, tool results, and usage meter
