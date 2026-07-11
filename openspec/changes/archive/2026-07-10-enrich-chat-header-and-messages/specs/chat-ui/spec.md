## MODIFIED Requirements

### Requirement: Multi-line Composer Input

The composer input SHALL be a multi-line text area. Pressing `Enter` SHALL submit the message; pressing `Shift+Enter` SHALL insert a newline without submitting. The composer SHALL make this binding discoverable by stating it in both the input placeholder and the composer fineprint.

#### Scenario: Shift+Enter inserts a newline

- **WHEN** the user presses `Shift+Enter` in the composer
- **THEN** a newline is inserted into the input and the message is not submitted

#### Scenario: Enter submits

- **WHEN** the user presses `Enter` (without Shift) on a non-empty input while not streaming
- **THEN** the message is submitted

#### Scenario: Textarea grows with content

- **WHEN** the input grows beyond one line
- **THEN** the textarea expands up to a bounded maximum height and then scrolls internally, and resets its height after submit

#### Scenario: The multi-line binding is discoverable

- **WHEN** the composer renders its placeholder and fineprint
- **THEN** both indicate that `Enter` sends and `Shift+Enter` inserts a newline

## ADDED Requirements

### Requirement: Chat Header Bar

The chat surface SHALL render a header bar across the top of the main thread column, above the message viewport, that persists across conversation turns. The header SHALL follow the application's standard header styling pattern. The agent selector SHALL be placed at the far-left of this header; the context-usage meter SHALL be placed at the far-right. The agent selector SHALL NOT also appear in the composer toolbar.

#### Scenario: The agent selector lives in the header

- **WHEN** the chat surface renders
- **THEN** the agent selector appears at the far-left of the chat header bar and is absent from the composer toolbar

#### Scenario: The header persists during streaming

- **WHEN** the agent is streaming a response
- **THEN** the chat header bar (and the agent selector within it) remains visible and is not replaced by streaming UI

### Requirement: Context Window Usage Meter

The chat header SHALL display a context-window usage meter showing how much of the active agent's context window the current conversation is consuming. The meter SHALL present a `used / max` readout alongside a progress bar whose fill width is the used-to-max ratio clamped to 100%. The `used` value SHALL be the most recent completed turn's `prompt_tokens` (representing the full prompt resent each turn, i.e. true context fill), and `max` SHALL be the active agent's resolved context-window size. The meter SHALL be hidden when the context window is unknown (zero).

#### Scenario: The meter updates after a turn

- **WHEN** a turn completes and the client receives the turn's token usage
- **THEN** the meter's `used` value reflects that turn's `prompt_tokens` against the agent's context window

#### Scenario: The meter shows the context window as the maximum

- **WHEN** the chat stream initializes
- **THEN** the meter's denominator is the agent's resolved context window, received before the first turn completes

#### Scenario: The meter shows on a loaded conversation

- **WHEN** the user opens an existing conversation from history
- **THEN** the meter reflects the last persisted turn's `prompt_tokens` against the context window without requiring a new turn

#### Scenario: The meter hides when the context window is unknown

- **WHEN** the active agent's context window cannot be determined (zero)
- **THEN** the meter is not rendered rather than dividing by zero

#### Scenario: The meter clamps a full window

- **WHEN** the used token count meets or exceeds the context window
- **THEN** the progress bar fills to 100% rather than overflowing

### Requirement: Context Usage Data over the Chat Stream

The chat streaming protocol SHALL surface the data the context meter requires. The `init` event SHALL include the active agent's resolved context-window size. The `turn` event SHALL include the turn's `prompt_tokens`, `completion_tokens`, and `total_tokens` in addition to the existing total. The persisted-turn payload served for conversation history SHALL include per-turn token counts and the agent's context window so the meter can render for loaded conversations.

#### Scenario: The init event carries the context window

- **WHEN** a chat stream begins
- **THEN** the `init` event includes the active agent's resolved context-window token size

#### Scenario: The turn event carries per-turn token usage

- **WHEN** a turn completes and its metadata is emitted
- **THEN** the `turn` event includes `prompt_tokens`, `completion_tokens`, and `total_tokens`

#### Scenario: History turns carry token usage

- **WHEN** the client loads a conversation's persisted turns
- **THEN** each turn exposes its token counts and the conversation exposes the agent's context window

### Requirement: Per-Message Timestamps

Each rendered chat message SHALL display its creation date and time. The optimistic user message SHALL be timestamped at submit time so its timestamp is visible immediately; messages loaded from history SHALL use the server-provided creation time. A message without a creation time (e.g. a streaming assistant message before reload) SHALL render no timestamp line rather than an empty or placeholder time.

#### Scenario: A user message shows the time it was sent

- **WHEN** the user submits a message
- **THEN** the message displays its send time immediately, derived from the client-set creation timestamp

#### Scenario: A history message shows its server time

- **WHEN** a message loaded from conversation history renders
- **THEN** it displays the server-provided creation date and time

#### Scenario: A streaming message shows no timestamp until reload

- **WHEN** an assistant message is actively streaming and has no creation time
- **THEN** no timestamp line is rendered for it
