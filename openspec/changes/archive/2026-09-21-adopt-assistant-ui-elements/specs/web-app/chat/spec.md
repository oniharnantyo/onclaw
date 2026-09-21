# web-app/chat — Delta

## MODIFIED Requirements

### Requirement: Context meter
For a 1:1 agent chat, the composer SHALL render a context ring in its left control rail — a small donut that fills clockwise with the share of the agent's effective context window the conversation currently fills, beside a monospace percentage. The ring's value SHALL be the latest turn's final-call input tokens divided by the agent's `effective_context_window`, updated when a terminal response event carries usage and restored from the thread's last turn on reload. The ring and percentage SHALL render in the accent tone below 65% of the window, amber from 65% to 85%, and the danger tone above 85%; the summarization trigger SHALL NOT be marked anywhere on the meter. Activating the ring SHALL open a popover above the composer rail containing: the exact used/window counts (`68k / 200k`) with a count-up animation on change; a segmented breakdown bar; a legend with one row per segment plus a Headroom row (window minus used, floored at zero); the last turn's input and output token rows when the wire reported them; and a percentage caption. The breakdown's segments — Instructions, Tools & skills, Files, Conversation — SHALL be shown at face value with an "estimated" caption; the Conversation segment SHALL count only transcript entries after the latest compaction divider; and a Server context segment (used minus the estimated segments, floored at zero) SHALL be shown with a hover explanation naming what it holds (this turn's retrieved memory, persona docs, tool schemas, compaction summaries). When the wire carries a server-provided breakdown, its numbers SHALL replace the client estimate; otherwise the estimate is the fallback, always labeled as estimated. Channels and direct member conversations SHALL NOT render the ring. When no turn has produced usage yet, or a terminal event arrives without a usage block, the ring SHALL be hidden rather than show a zero or a fabricated value. The chat header SHALL NOT render a context meter.

#### Scenario: Meter fills per turn
- **WHEN** a turn completes on an agent chat whose usage reports 68,000 final-call input tokens and whose agent exposes an effective window of 200,000
- **THEN** the composer ring shows 34% and, in the popover, `68k / 200k`

#### Scenario: Warn at the summarization trigger
- **WHEN** the ring's value reaches or exceeds the agent's `summarization_trigger_tokens`
- **THEN** the ring keeps rendering the plain 65/85 severity ladder and carries no trigger tick or trigger-specific marking

#### Scenario: Meter survives reload
- **WHEN** a user reopens a thread whose last turn carried usage
- **THEN** the ring shows that turn's final-call input against the effective window without waiting for a new turn

#### Scenario: Hidden outside agent chats
- **WHEN** the composer renders for a channel or a direct member conversation
- **THEN** no context ring appears and the header renders no meter

#### Scenario: Hidden without usage data
- **WHEN** a fresh thread has no turns yet, or the latest terminal event carries no usage block
- **THEN** no ring, popover, or placeholder value appears anywhere

#### Scenario: Ring fills per turn with severity tiers
- **WHEN** turns complete on an agent chat against a 200,000-token effective window, first at 68,000 then at 140,000 then at 180,000 final-call input tokens
- **THEN** the composer ring shows 34% in the accent tone, then 70% in amber, then 90% in the danger tone

#### Scenario: Breakdown popover opens upward
- **WHEN** the user activates the ring
- **THEN** a popover opens above the composer rail with the used/window counts, the segmented bar, the legend rows, Headroom, and the estimated caption

#### Scenario: Server context row explains the unmeasured share
- **WHEN** the popover renders with client estimates summing to less than the used total
- **THEN** a Server context row shows the remainder with a hover explanation, and Headroom shows the window minus used

#### Scenario: Server-provided breakdown wins when present
- **WHEN** the last turn's usage carries a server-provided context breakdown
- **THEN** the legend shows the server numbers without the estimated caption for those segments

#### Scenario: Conversation segment respects compaction
- **WHEN** the transcript contains a compaction divider followed by newer entries
- **THEN** the Conversation estimate counts only the entries after the divider

#### Scenario: Turn input and output rows render from real usage
- **WHEN** the last terminal event reported input and output tokens
- **THEN** the popover shows both counts, and omits the rows when the wire reported none

#### Scenario: Ring survives reload
- **WHEN** a user reopens a thread whose last turn carried usage
- **THEN** the ring shows that turn's final-call input against the effective window without waiting for a new turn

#### Scenario: Hidden outside agent chats and without usage
- **WHEN** the composer renders for a channel or a direct member conversation, or the thread has no usage yet
- **THEN** no ring, popover, or placeholder value appears, and the header renders no meter

## ADDED Requirements

### Requirement: Message queue
While a run is active in an agent chat, a message the user sends SHALL join a visible queue rendered between the transcript and the composer: a running row naming the in-flight turn and one queued row per pending message showing its order, its text, and a remove control. Removing a queued entry SHALL cancel only that entry. When the active run finishes, the first queued message SHALL dispatch automatically in order, without user action. Queue rendering is present-only: no queue chrome appears when nothing is queued. Conflict queueing that originates outside this tab (another tab, scheduler, cron) SHALL keep the existing catch-up-and-redispatch behavior.

#### Scenario: Send during a run queues visibly
- **WHEN** the user sends two messages while a turn is streaming
- **THEN** both appear as ordered cancelable queued rows under a running row

#### Scenario: Cancel removes only that entry
- **WHEN** the user removes the second queued message
- **THEN** it is dropped and the first still dispatches when the run finishes

#### Scenario: Automatic dispatch on completion
- **WHEN** the active run finishes with a queued message pending
- **THEN** the queued message dispatches as a normal turn and the queue rows clear

### Requirement: Draft restore
Unsent composer text SHALL persist per thread. Returning to a thread with a saved draft SHALL restore it into the composer; sending SHALL clear the saved draft. Drafts are local to the user's client and SHALL NOT sync across devices or appear in any API surface.

#### Scenario: Draft survives leaving and returning
- **WHEN** the user types text, navigates to another thread, and returns
- **THEN** the composer shows the unsent text

#### Scenario: Send clears the draft
- **WHEN** a restored draft is sent
- **THEN** the saved draft is cleared and a later visit shows an empty composer

### Requirement: Message timing
A completed assistant reply from a live turn MAY carry client-measured timing — time to first streamed token, total turn time, and streamed tokens per second — revealed on hover near the message actions. Hydrated history SHALL render no timing line. Timing SHALL NOT render while the turn is still streaming.

#### Scenario: Hover reveals timing on a live turn
- **WHEN** a turn streamed text and completes
- **THEN** hovering the reply's action row shows first-token, total, and speed figures

#### Scenario: Hydrated replies carry no timing
- **WHEN** a thread is reloaded from history
- **THEN** no reply shows a timing line

### Requirement: Day separators and hover timestamps
The transcript SHALL render a date divider whenever the calendar day changes between consecutive dated entries, labeling today and yesterday by name and older days by date. Entries with parseable dates SHALL expose their full date and time on hover. Entries without parseable dates SHALL render without contributing a divider.

#### Scenario: Divider at a day boundary
- **WHEN** consecutive messages fall on different calendar days
- **THEN** a full-width divider labels the new day (Today, Yesterday, or the date) between them

#### Scenario: Undated entries stay silent
- **WHEN** a transcript entry carries no parseable date
- **THEN** it renders without a divider and without a hover timestamp

### Requirement: Tool group timeline collapse
An assistant turn containing four or more tool calls SHALL render a collapsible timeline header instead of the inline card stack: a resting summary of step count and changed-file churn (derived from file-edit results) that expands to reveal the familiar inline cards in stream order. Turns with fewer than four tool calls SHALL render inline cards directly, and cards MUST NOT be muted or hidden behind the collapse for such turns. Expanding or collapsing SHALL be user-controlled state, not automatic.

#### Scenario: Heavy turn collapses by default
- **WHEN** a turn executes six tool calls including two file edits
- **THEN** the turn renders the collapsed header ("6 steps · 2 files changed") and the inline cards appear only on expand

#### Scenario: Light turns keep inline cards
- **WHEN** a turn executes two tool calls
- **THEN** the cards render inline exactly as before, with no collapse header
