## ADDED Requirements

### Requirement: Conversation history is owned by the business layer, not the runner

Following the Eino best-practice separation (framework vs business layer), conversation-history load and turn persistence SHALL be owned by a business-layer component (`SessionManager`) that holds the `ConversationStore`. The runner (`Agent.Run`) SHALL receive the assembled message list (replayed history plus the new user message) and SHALL NOT load, inject, or persist history itself. The business-layer entrypoints SHALL drive the loop: load history, assemble the message list, run, drain, and read committed turn metadata. A framework-side turn collector MAY accumulate the turn from agent state and hand it to the business layer for persistence, but it SHALL NOT hold the store or write rows.

#### Scenario: The runner receives the assembled message list

- **WHEN** an entrypoint begins a turn
- **THEN** it loads history via the business-layer `SessionManager`, assembles `history + new user message`, and passes that list to `Agent.Run`
- **AND** the runner does not load or persist history

#### Scenario: Persistence is owned by the business layer

- **WHEN** a turn completes and is committed
- **THEN** the `ConversationStore.AppendTurn` call is made by the business-layer `SessionManager`
- **AND** no framework middleware holds the store or writes a turn row

## MODIFIED Requirements

### Requirement: A turn is committed as one row at turn end

The system SHALL commit a turn as a single row when the turn completes, accumulating the turn's new messages in memory during the run and writing them once through the business-layer `SessionManager` (not the run loop, not eagerly per message). A framework turn collector SHALL gather the turn's new messages from agent state and hand them to the `SessionManager`, which performs redaction, response-ID and token extraction, marshaling, and the `AppendTurn` write under a context detached from the run lifecycle. The committed row SHALL record the model, per-turn token usage read from the final assistant message's response metadata, the `response_id` of the turn, the `previous_response_id` of the prior turn (empty for the first turn), and extracted `question`/`answer` text.

#### Scenario: A complete turn is committed once

- **WHEN** a turn completes with a final assistant response
- **THEN** exactly one turn row is written containing all of the turn's messages, model, token usage, response ids, and question/answer
- **AND** the write is performed by the business-layer `SessionManager`, not by a framework middleware

#### Scenario: A turn interrupted before completion leaves no row

- **WHEN** a turn is interrupted before the turn-end handoff fires
- **THEN** no turn row is written for that turn (a turn is a complete exchange ending in a response)