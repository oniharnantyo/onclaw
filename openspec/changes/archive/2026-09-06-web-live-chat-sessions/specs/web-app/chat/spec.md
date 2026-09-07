# web-app/chat Delta — web-live-chat-sessions

## MODIFIED Requirements

### Requirement: Message send and simulated response
Sending a message SHALL append it to the active session, derive the session title from the first user message (truncated at 42 characters), and produce the agent's reply through the chat runtime bridge. The reply SHALL stream into the transcript incrementally, with a running indicator while the turn is in flight. While a turn is in flight the send control SHALL become a stop control that cancels the turn; partial text SHALL remain in the transcript. Teammate direct messages SHALL NOT trigger an agent turn. In the live UI the reply SHALL always come from the agent runtime — canned simulated replies SHALL NOT be produced (they remain available to test fixtures only); when live chat is unavailable the connect state governs instead.

#### Scenario: Direct agent chat
- **WHEN** the user sends a message to agent "Atlas"
- **THEN** a running indicator appears, then Atlas's real reply streams in incrementally

#### Scenario: Teammate direct message
- **WHEN** the user sends a message in a teammate DM
- **THEN** the message is appended and no agent turn occurs

#### Scenario: Stop control
- **WHEN** the user presses stop while Atlas is responding
- **THEN** the turn cancels, the running indicator clears, and partial text remains

#### Scenario: No canned fallback
- **WHEN** live chat is unavailable and the user sends a message to an agent
- **THEN** no simulated reply is generated; the connect state is shown instead
