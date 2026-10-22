# Spec Delta

## ADDED Requirements

### Requirement: Tool-output reduction

A turn SHALL bound the model-facing size of tool results. A tool output exceeding the configured length bound SHALL be stored inside the agent's workspace, and the result carried on the transcript SHALL become a short notice naming the stored file, so the model can read the full content back through its file tools. When a turn's accumulated working state crosses its share of the model's context limit, older tool arguments and results SHALL likewise be offloaded to the workspace while the most recent tool-call rounds stay intact. Reduction MUST NOT fail the turn: any failure while offloading leaves the original content in place.

#### Scenario: Oversized output becomes a readable file

- **WHEN** a tool returns output larger than the configured bound
- **THEN** the full content is stored in the agent's workspace, the transcript carries a notice with the stored path, and the model can read the content back through its file tools

#### Scenario: Old tool payloads clear under budget pressure

- **WHEN** a turn's working state crosses the context budget and older tool calls exist beyond the retained recent rounds
- **THEN** those arguments and results are offloaded to the workspace and the turn continues within the limit

#### Scenario: Offload failure keeps the original

- **WHEN** storing an oversized output fails
- **THEN** the original tool result stays on the transcript and the turn proceeds

### Requirement: In-run context safety net

Checkpoint compaction remains the persisted cross-turn mechanism. Additionally, when a single turn's working state crosses a safety threshold of the model's context limit mid-turn, the runtime SHALL summarize that state and continue the turn within the limit. An in-run summary is ephemeral: it MUST NOT be persisted as the session checkpoint, MUST NOT advance the checkpoint boundary, and the stored transcript MUST NOT be truncated.

#### Scenario: Long turn stays within the limit

- **WHEN** a single turn's accumulated state crosses the safety threshold before the model produces its answer
- **THEN** the state is summarized in-run, the turn continues, and it completes within the context limit

#### Scenario: Checkpoint compaction unchanged

- **WHEN** a session's context crosses the compaction threshold between turns
- **THEN** the runtime compacts into a persisted checkpoint exactly as before, regardless of any in-run summary
