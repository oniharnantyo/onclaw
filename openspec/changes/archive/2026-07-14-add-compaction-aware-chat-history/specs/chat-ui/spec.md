## ADDED Requirements

### Requirement: Compaction Boundary Marker
The system SHALL render a flagged summary turn as a compaction boundary marker — a divider indicating earlier conversation was summarized, with the summary text available in a collapsible region — and SHALL NOT render it as a normal assistant message. Every flagged summary row SHALL render as its own marker, so re-compaction shows the full compaction history. Non-summary messages SHALL remain fully visible; append-only retention is reflected in the transcript unchanged.

#### Scenario: A summary renders as a marker, not a bubble
- **WHEN** a conversation contains a flagged summary turn between older and newer messages
- **THEN** the summary renders as a divider marker with collapsible summary text
- **AND** it does not render as a flat assistant message

#### Scenario: Older messages remain visible across a compaction
- **WHEN** a compaction marker is rendered
- **THEN** all non-summary messages before and after it remain fully visible

#### Scenario: Re-compaction shows multiple markers
- **WHEN** a conversation has been compacted more than once
- **THEN** each flagged summary renders as its own marker in sequence order

### Requirement: Context Meter Annotates Compaction
The context-window usage meter SHALL display a one-time compaction annotation when the conversation's `compaction_count` increases, so the post-compaction drop in `used` reads as a compaction event rather than a glitch. The meter SHALL continue to display true context fill (the most recent turn's `prompt_tokens`); the annotation explains the drop, it does not redefine the metric. The meter's `used` source SHALL NOT be anchored on a summary row.

#### Scenario: The meter annotates a compaction
- **WHEN** a turn completes and `compaction_count` has increased since the prior turn
- **THEN** the meter shows a one-time compaction annotation alongside the dropped `used` value

#### Scenario: The meter does not anchor on a summary row
- **WHEN** the last persisted row is a summary row (e.g. a turn that did not complete)
- **THEN** the meter's `used` source skips the summary row rather than reading zero