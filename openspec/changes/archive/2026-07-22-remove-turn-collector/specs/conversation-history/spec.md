## ADDED Requirements

### Requirement: Turn persistence is driven by the entrypoint, not a framework middleware

The agent runner SHALL contain no persistence middleware. The turn SHALL be reconstructed by the entrypoint from the agent event stream (all message outputs, with streamed assistant messages reassembled into their complete form) and committed via `SessionManager.CommitTurn` after the entrypoint drains the run. No component within the agent middleware chain SHALL call `CommitTurn` or hold the conversation store.

#### Scenario: The entrypoint commits the reconstructed turn

- **WHEN** a turn completes and the entrypoint has drained the event stream
- **THEN** the entrypoint calls `SessionManager.CommitTurn` with the turn messages collected from the event stream
- **AND** no agent middleware participates in persistence

## MODIFIED Requirements

### Requirement: Per-turn model and token usage are recorded

Each persisted turn row SHALL record the model used for the turn and the turn's `prompt_tokens`, `completion_tokens`, and `total_tokens`. Because token usage is no longer read from framework `ResponseMeta`, these values SHALL be a **business-layer char-based estimate** over the turn's messages (the same chars-per-token heuristic used elsewhere in the system), not provider-reported usage. The estimate SHALL be deterministic for the same input.

#### Scenario: Token usage is estimated, not provider-reported

- **WHEN** a turn completes and is committed
- **THEN** the turn row's token columns reflect a char-based estimate of the turn's messages
- **AND** the values are not sourced from provider response metadata

### Requirement: Response ids thread follow-up turns

Each turn row SHALL carry a `response_id` and a `previous_response_id` equal to the prior turn's `response_id` (empty for the first turn of a conversation). Because provider-extension response ids (OpenAI/Gemini) are only present in framework state and are no longer read, the `response_id` SHALL be derived from the Eino framework message id (`_eino_msg_id`) carried on the streamed final assistant message's `Extra` where available, and SHALL be empty otherwise. The system SHALL log a warning when no response id is available. These ids are surfaced via the chat API for client chaining and do not alter live history reconstruction.

#### Scenario: Response id uses the Eino message id fallback

- **WHEN** a turn's final assistant message carries `_eino_msg_id` in its `Extra`
- **THEN** the turn row's `response_id` is that id

#### Scenario: Empty response id is permitted when unavailable

- **WHEN** no `_eino_msg_id` is present on the final assistant message
- **THEN** the turn row's `response_id` is empty and a warning is logged
- **AND** the turn is still committed (provider-extension ids are no longer required)