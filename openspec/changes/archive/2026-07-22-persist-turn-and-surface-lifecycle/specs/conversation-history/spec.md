## ADDED Requirements

### Requirement: Turn history is persisted on abnormal terminal states

The agent SHALL persist the partial turn to `conversation_messages` when a turn terminates via model/agent error, interrupt, or context cancellation — not only on successful completion. Because the underlying agent framework invokes the history middleware's completion hook solely on successful terminals and exposes no error/finalize hook, the agent layer SHALL trigger persistence itself from the iterator's abnormal-terminal branches.

#### Scenario: An errored turn is still persisted

- **WHEN** a turn's model call errors mid-turn and the iterator terminates with that error
- **THEN** exactly one `conversation_messages` row is written containing the messages accumulated up to the error

#### Scenario: A cancelled turn is persisted with its partial content

- **WHEN** the user cancels the turn (context cancellation) after partial assistant output
- **THEN** the partial turn is persisted; the row's `response_id`/token fields degrade gracefully (empty/zero) when unavailable

#### Scenario: An interrupted turn is persisted

- **WHEN** the agent terminates with an interrupt action
- **THEN** the buffered turn is persisted

#### Scenario: Normal completion is not double-persisted

- **WHEN** a turn completes successfully
- **THEN** exactly one row is written (via the framework's completion hook), and the abnormal-terminal persistence path does not fire
