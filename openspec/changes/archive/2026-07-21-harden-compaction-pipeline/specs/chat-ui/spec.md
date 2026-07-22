## ADDED Requirements

### Requirement: Context usage updates during the run
The chat SHALL update the context meter as the run unfolds, not only at turn end. The backend SHALL emit a per-step usage event over the chat SSE stream carrying the latest model call's token usage, and the client SHALL apply it to the meter so a multi-step run visibly fills the meter. A zero prompt-token count SHALL fall back to the total-token count rather than zeroing the meter.

#### Scenario: The meter ticks during a multi-step run
- **WHEN** an agent run makes several model calls
- **THEN** the context meter updates after each call, not only when the run completes

### Requirement: Compaction is indicated live
The chat SHALL indicate to the user while summarization compaction is running. The backend SHALL emit compaction start/end events over the chat SSE stream, and the client SHALL render a visible "compacting context" state during that window, so a long summarizer call is not indistinguishable from a generic loading spinner.

#### Scenario: A compaction shows an indicator
- **WHEN** summarization compaction runs during a turn
- **THEN** the UI displays a compaction indicator for the duration and clears it when compaction ends