# web-app/chat-runtime Delta — web-live-chat-sessions

## ADDED Requirements

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
