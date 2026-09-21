# agent-runtime — Delta

## ADDED Requirements

### Requirement: Context breakdown measurement
The runner MAY attach a `context_breakdown` block to a turn's usage when the turn's provider reported usage, splitting the final call's input into labeled segments: `instructions` (the composed instruction: persona, workspace and user docs, memory sections, channel docs, profiles), `tools` (marshaled tool schemas), `conversation` (the session window's messages), `files` (in-window attachments), and `server` (the composed share not attributable to the other segments). Section sizes SHALL be measured at composition time from the real composed strings, and the conversation segment SHALL be measured over the true session window at turn end using the same display-grade estimator the summarization middleware uses (~4 characters per token). The breakdown is display-only context diagnostics: it MUST NOT feed billing, trigger math, or summarization decisions, and a turn whose provider reported no usage SHALL omit the block entirely. Backends unable to measure a segment SHALL omit that segment rather than report zero.

#### Scenario: Breakdown accompanies turn usage
- **WHEN** a turn with a composed instruction and tool schemas completes with provider usage
- **THEN** the turn's usage carries labeled segment counts whose sections sum to no more than the final-call input, with any unattributable share in `server`

#### Scenario: Conversation measured over the true window
- **WHEN** a turn runs after compaction replaced part of the window with a summary
- **THEN** the `conversation` segment reflects the actual session window, not the full transcript

#### Scenario: Display-only by contract
- **WHEN** the breakdown is computed
- **THEN** changing or removing it changes no billing, trigger, or summarization behavior
