## MODIFIED Requirements

### Requirement: Per-turn usage capture
The runtime SHALL capture token usage per execution turn — input tokens, output tokens, and their total, as reported by the model provider — and SHALL expose the totals on the turn's terminal transcript event. Usage SHALL be captured whether or not any consumer is attached to the live stream, and SHALL persist with the turn's history. The capture SHALL additionally record the **final-call input tokens**: the input token count of the turn's last model call, representing the context size the model last saw. The plain input total accumulates every model call in the turn, so on a multi-call turn it exceeds the final-call count; both are reported independently.

#### Scenario: Usage reported at turn end
- **WHEN** an execution reaches its terminal event
- **THEN** the terminal event carries the turn's input/output/total token counts as reported by the provider

#### Scenario: Usage persists with history
- **WHEN** a turn's transcript is reloaded from persisted history
- **THEN** the turn's usage totals are retrievable from durable data

#### Scenario: Final-call input diverges from accumulated total
- **WHEN** a turn makes three model calls whose provider-reported inputs are 40,000, 45,000, and 50,000 tokens
- **THEN** the turn's usage reports an input total of 135,000 and final-call input of 50,000

#### Scenario: Single-call turn matches
- **WHEN** a turn makes exactly one model call whose provider-reported input is 1,200 tokens
- **THEN** the turn's usage reports input total 1,200 and final-call input 1,200

#### Scenario: Final-call input survives reload
- **WHEN** a multi-call turn's transcript is reloaded from persisted history
- **THEN** the turn's final-call input count is rebuilt from the persisted record and equals the last model call's input
