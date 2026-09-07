## ADDED Requirements

### Requirement: Context meter
For a 1:1 agent chat, the chat header SHALL render a context meter in the top-right control row — a compact bar plus a monospace percentage indicating how much of the agent's effective context window the conversation currently fills. The meter's value SHALL be the latest turn's final-call input tokens divided by the agent's `effective_context_window`, updated when a terminal response event carries usage and restored from the thread's last turn on reload. Hovering SHALL reveal the exact counts (used and window, e.g. `68k / 200k`). The meter SHALL turn amber at or above `summarization_trigger_tokens`. Channels and direct member messages SHALL NOT render a meter. When no turn has produced usage yet, or a terminal event arrives without a usage block, the meter SHALL be hidden rather than show a zero or a fabricated value.

#### Scenario: Meter fills per turn
- **WHEN** a turn completes on an agent chat whose usage reports 68,000 final-call input tokens and whose agent exposes an effective window of 200,000
- **THEN** the header meter shows 34% and, on hover, `68k / 200k`

#### Scenario: Warn at the summarization trigger
- **WHEN** the meter's value reaches or exceeds the agent's `summarization_trigger_tokens`
- **THEN** the meter renders in the amber warn state

#### Scenario: Meter survives reload
- **WHEN** a user reopens a thread whose last turn carried usage
- **THEN** the meter shows that turn's final-call input against the effective window without waiting for a new turn

#### Scenario: Hidden outside agent chats
- **WHEN** the header renders for a channel or a direct member conversation
- **THEN** no context meter appears

#### Scenario: Hidden without usage data
- **WHEN** a fresh thread has no turns yet, or the latest terminal event carries no usage block
- **THEN** the header renders no meter and no placeholder value
