# agent-runtime Delta

## ADDED Requirements

### Requirement: Per-turn usage capture
The runtime SHALL capture token usage per execution turn — input tokens, output tokens, and their total, as reported by the model provider — and SHALL expose the totals on the turn's terminal transcript event. Usage SHALL be captured whether or not any consumer is attached to the live stream, and SHALL persist with the turn's history.

#### Scenario: Usage reported at turn end
- **WHEN** an execution reaches its terminal event
- **THEN** the terminal event carries the turn's input/output/total token counts as reported by the provider

#### Scenario: Usage persists with history
- **WHEN** a turn's transcript is reloaded from persisted history
- **THEN** the turn's usage totals are retrievable from durable data
