## ADDED Requirements

### Requirement: Compact command menu
Typing `/` at the start of an agent-chat composer SHALL open a command menu listing only `/compact` (with its description), navigable by arrow keys with Enter completing the command. Submitting `/compact` — with optional trailing text as the focus instruction — SHALL be intercepted by the client and submitted as a compact-command turn; the literal command text SHALL NOT reach the model as a user message and SHALL NOT be persisted as one. In channel and team composers the command menu SHALL NOT open and `/compact` SHALL send as ordinary text. Any other `/foo` input SHALL send as ordinary text in every surface.

#### Scenario: Menu lists only the compact command
- **WHEN** the user types `/` in an agent-chat composer
- **THEN** the menu shows only `/compact` — none of the former `/tools`, `/model`, `/schedule`, `/reset`, `/help` entries appear

#### Scenario: Command is intercepted
- **WHEN** the user submits `/compact keep the API design decisions`
- **THEN** a compact-command turn is submitted with that focus text, no user message containing the command appears in the thread, and the model receives no literal `/compact` text

#### Scenario: Unknown slash input passes through
- **WHEN** the user submits `/frobnicate` in an agent chat
- **THEN** it sends as an ordinary user message

#### Scenario: Channels do not offer commands
- **WHEN** the user types `/compact` in a channel composer
- **THEN** no command menu opens and the text sends as an ordinary message

### Requirement: Compaction status row
While a compact-command turn is running in an agent chat, the thread SHALL show a pending status row ("Compacting context…") in place of any agent reply, and no user message for the command SHALL appear. The status row SHALL be retracted when the turn reaches a terminal state — replaced by the compaction divider on success, and removed without a divider when the turn fails or completes without compacting (error surfacing follows the turn-failure rules).

#### Scenario: Status row during compaction
- **WHEN** the user submits `/compact` and the summarizer is still running
- **THEN** the thread shows the compacting status row with no user pill for the command and no optimistic agent reply

#### Scenario: Status row retracted on quiet completion
- **WHEN** a compact turn completes without producing a compaction (nothing to compact) or fails
- **THEN** the status row is removed and no divider is left behind

### Requirement: Compaction divider
A completed context compaction — manual (`/compact`) or automatic (threshold-triggered) — SHALL render in the transcript as a divider between the older messages and the compacted window, showing `Context compacted` with the working-context token estimates before → after when available. The divider SHALL render identically for live turns and for hydrated history, so past compactions remain visible after reload. The UI SHALL NOT fabricate an agent acknowledgement message for a compaction.

#### Scenario: Manual compaction divider
- **WHEN** a `/compact` turn completes with the context-compacted event carrying token estimates
- **THEN** the transcript shows a `Context compacted · N → M tokens` divider between the prior messages and the compacted window, with no user pill and no agent ack message

#### Scenario: Automatic compaction is visible
- **WHEN** an automatic threshold compaction occurs mid-conversation
- **THEN** the same divider renders at the compaction point

#### Scenario: Divider survives reload
- **WHEN** the user reloads a chat whose session history contains a compaction
- **THEN** the hydrated transcript renders the same divider at the recorded position

## REMOVED Requirements

### Requirement: Slash commands
**Reason**: The scripted-reply command set (`/tools`, `/model`, `/schedule`, `/reset`, `/help`) was prototype-era fiction — picks inserted text that reached the model as an ordinary user message, so the "scripted replies" never came from the app. Replaced wholesale by the compact command menu, the only command executed for real.
**Migration**: None — the removed commands performed no real work. Users lose nothing functional; `/` now offers the one command that actually executes. Skills remain available via `$name` invocation.
