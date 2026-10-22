# Spec Delta

## Purpose

Provide the chat menu: per-agent session lists plus the chat view with its right sidebar, porting onclaw-v2's chat affordances (context ring, composer menus, tool-call cards, reasoning bubbles, compaction markers) so single-user chat matches the predecessor's capabilities.

## ADDED Requirements

### Requirement: Sessions organized per agent

The chat menu SHALL list agents and, for each agent, its sessions with latest activity. The user SHALL be able to start a new session for an agent and switch between sessions without losing history.

#### Scenario: Start a new session

- **WHEN** the user selects an agent and creates a new session
- **THEN** an empty chat view opens for that agent and the session appears in the agent's session list

#### Scenario: Switch sessions

- **WHEN** the user switches from one session to another of the same agent
- **THEN** the transcript of the selected session is displayed

### Requirement: Chat transcript rendering

The chat view SHALL render user messages, assistant messages, reasoning bubbles, tool-call cards with their results, and compaction markers in chronological order. Streaming output MUST be appended live during a turn.

#### Scenario: Tool call visible in transcript

- **WHEN** an agent turn includes tool activity
- **THEN** the transcript shows a tool-call card with the tool name and its result inline in message order

#### Scenario: Live streaming in transcript

- **WHEN** an agent is responding
- **THEN** partial text appears in the transcript as it streams

### Requirement: Chat right sidebar

The chat view SHALL provide a right sidebar showing the current session's details: the agent (with its avatar), the model in use, and the context-window usage indicator. Collapsing the sidebar MUST NOT interrupt an in-flight turn.

#### Scenario: Context indicator

- **WHEN** the user views a session with accumulated turns
- **THEN** the right sidebar shows context usage relative to the model's limit

### Requirement: Composer affordances

The message composer SHALL support sending with Enter, newline insertion, slash commands, a skill picker, and an @-mention menu, matching onclaw-v2's composer behavior. Queued messages sent while the agent is busy MUST be delivered in order.

#### Scenario: Slash command menu

- **WHEN** the user types "/" in the composer
- **THEN** a menu of available slash commands opens and filters as the user types

#### Scenario: Message queued while busy

- **WHEN** the user sends a message while the agent is still responding
- **THEN** the message is queued and delivered after the current turn completes, in send order

### Requirement: Theme parity

The chat menu SHALL render correctly in both the light and dark daisyUI themes, including transcripts, sidebar, composer menus, and tool cards.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while viewing a chat
- **THEN** every chat view element renders legibly in the selected theme
