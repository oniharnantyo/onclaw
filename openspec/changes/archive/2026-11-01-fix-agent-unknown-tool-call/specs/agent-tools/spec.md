# Spec Delta

## MODIFIED Requirements

### Requirement: Tool execution loop

A turn SHALL resolve the agent's tool surface, offer it to the model, execute the tool calls the model makes, feed each result back, and continue until the model answers without tool calls or an iteration cap is reached. Each tool call and its result SHALL be recorded on the session transcript as it happens, with the result content persisted. When the iteration cap is reached, the turn SHALL end with a visible notice instead of failing silently. A tool call naming a tool outside the offered surface SHALL be answered with an error tool result naming the unknown tool — the turn SHALL NOT end because of it, and the model SHALL be able to retry with a valid name. Each unknown call SHALL appear on the transcript exactly once, as a failed tool record.

#### Scenario: Tool call round trip

- **WHEN** the model calls a tool mid-turn
- **THEN** the tool executes, the result is fed back to the model, and the turn continues toward a final answer

#### Scenario: Runaway loop is bounded

- **WHEN** a turn's tool-call iterations exceed the cap
- **THEN** the turn ends with a visible cap notice rather than continuing indefinitely

#### Scenario: Tool failure feeds back

- **WHEN** a tool invocation fails
- **THEN** the error is returned to the model as the tool result and the turn continues

#### Scenario: Unknown tool name feeds back

- **WHEN** the model calls a tool name that was not offered in the turn's tool surface — for example `list` instead of `ls`
- **THEN** the send does not fail, the unknown call is answered with an error tool result naming it, the other calls in the same response execute normally, and the turn continues

#### Scenario: Unknown call recorded once

- **WHEN** a model response contains an unknown tool call
- **THEN** the session transcript shows exactly one failed tool record for it, and no duplicate record for the same call id
