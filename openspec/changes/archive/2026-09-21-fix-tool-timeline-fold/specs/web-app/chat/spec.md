# web-app/chat — Delta

## MODIFIED Requirements

### Requirement: Tool group timeline collapse
An assistant turn containing four or more tool calls SHALL render a collapsible timeline header instead of the inline card stack. The fold SHALL own every activity row of the turn except the reply text: expanding reveals the familiar inline cards AND the reasoning rows in stream order, nested visually under the header behind a thin left rail so the header reads as their parent; collapsed shows only the header and the reply text, with no reasoning rows visible outside the fold. The resting header summary SHALL count steps as tool calls only — folded reasoning rows SHALL NOT change the count. While the turn is streaming, the header SHALL render the live activity status defined by the Turn activity status line requirement. Turns with fewer than four tool calls SHALL render inline cards and reasoning rows directly, and cards MUST NOT be muted or hidden behind a collapse for such turns. Expanding or collapsing SHALL be user-controlled state, not automatic. A hydrated message rendered from the legacy flat reasoning field SHALL follow the same fold rule.

#### Scenario: Heavy turn collapses by default
- **WHEN** a turn executes six tool calls interleaved with reasoning segments, including two file edits
- **THEN** the turn renders the collapsed header ("6 steps · 2 files changed") with only the reply text below it — no tool cards and no Thought rows are visible until expand

#### Scenario: Expanded reveals cards and thoughts nested under the header
- **WHEN** the user expands a heavy turn's fold
- **THEN** the tool cards and Thought rows render interleaved in stream order, indented beneath the header behind a thin left rail — not at the same visual level as the header itself

#### Scenario: Resting count ignores folded thoughts
- **WHEN** a completed turn holds six tool calls and five reasoning segments
- **THEN** the header reads "6 steps" (plus the file clause when file edits succeeded), not a count of all rows

#### Scenario: Light turns keep inline cards
- **WHEN** a turn executes two tool calls with reasoning
- **THEN** the cards and Thought rows render inline exactly as before, with no collapse header

#### Scenario: Legacy reasoning follows the fold
- **WHEN** a hydrated heavy turn carries reasoning only in the legacy flat field
- **THEN** its reasoning row is hidden while the fold is collapsed and revealed on expand, like interleaved reasoning rows

## ADDED Requirements

### Requirement: Turn activity status line
While an agent turn is streaming on a heavy turn, the timeline header SHALL render a live activity status: a shimmering label naming the current activity — "Thinking" when no tool call is pending, "Running" followed by the tool's human-readable display name when a tool call is pending — accompanied by the turn's client-measured elapsed time. The label SHALL update as the activity changes and the shimmer SHALL replay on each label change. When the turn stops streaming, the status SHALL yield to the resting summary and no shimmer SHALL remain. Elapsed time is present-only: it SHALL render for live turns only, and hydrated history SHALL show neither elapsed time nor shimmer. Under a reduced-motion preference the label SHALL render as static text without the shimmer animation while keeping the same wording and elapsed time. The pre-first-token row shown while the agent has produced nothing SHALL use the same vocabulary — a shimmering "Thinking" label with elapsed time — in place of a bare pulsing dot.

#### Scenario: Header names the pending tool
- **WHEN** a heavy turn is streaming with a pending shell tool call and the fold is collapsed
- **THEN** the header shows the shimmering label "Running Shell" with elapsed time, and no orphaned rows render below it

#### Scenario: Header shows thinking between tool calls
- **WHEN** a heavy turn is streaming while the model generates with no tool call pending
- **THEN** the header shows the shimmering label "Thinking" with elapsed time

#### Scenario: Status yields at rest
- **WHEN** a heavy streaming turn completes
- **THEN** the header renders the resting summary ("N steps · M files changed") with no shimmer and no activity label

#### Scenario: Hydrated history stays at rest
- **WHEN** a thread reloads and renders a heavy collapsed turn from history
- **THEN** the header shows the resting summary with no shimmer and no elapsed time

#### Scenario: Reduced motion keeps the wording
- **WHEN** the user's system prefers reduced motion and a heavy turn streams
- **THEN** the header shows the same activity label and elapsed time as static text, without the shimmer sweep

#### Scenario: Pre-first-token row joins the vocabulary
- **WHEN** the user sends a message and the agent has produced no content yet
- **THEN** the waiting row shows a shimmering "Thinking" label with elapsed time instead of a bare pulsing dot
