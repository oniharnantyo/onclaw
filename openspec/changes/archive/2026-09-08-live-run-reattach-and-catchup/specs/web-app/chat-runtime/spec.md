## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Active run re-attachment on chat load
When a chat view or thread mounts with an active bound server session (`sess_<uuid>`), the client runtime SHALL check if an in-flight run is active or unfinished. If an execution is in progress, the client SHALL initiate a streaming connection to catch up missed events and attach to the live stream, streaming reasoning deltas, tool cards, and text deltas directly into the active assistant message until the turn completes.

#### Scenario: Page reload mid-turn resumes live stream
- **WHEN** the user refreshes the browser while the agent is executing tools or generating text
- **THEN** the chat re-attaches to the ongoing execution, renders completed tool cards and reasoning parts, and continues streaming new deltas to completion without manual user intervention

#### Scenario: Page reload after turn completed renders full response
- **WHEN** the user refreshes the browser and returns after the agent execution has finished
- **THEN** the hydration catches up all persisted events and renders the complete message, tool results, and usage meter
