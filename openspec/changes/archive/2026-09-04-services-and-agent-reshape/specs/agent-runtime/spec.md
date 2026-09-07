## ADDED Requirements

### Requirement: Bounded execution loop
An agent execution's reasoning loop SHALL be bounded by a server-configured maximum number of model iterations. When a turn would exceed the bound — the model keeps requesting tool calls without reaching a final answer — the runtime SHALL terminate the turn with an error outcome (surfaced as the terminal error transcript event) instead of continuing indefinitely. Normal turns that conclude within the bound SHALL be unaffected.

#### Scenario: Runaway loop terminates
- **WHEN** an agent's model keeps issuing tool calls beyond the maximum iteration count without producing a final answer
- **THEN** the turn ends with the terminal error transcript event and no further model calls are made

#### Scenario: Converging turn unaffected
- **WHEN** a turn reaches its final answer within the maximum iteration count
- **THEN** the execution completes normally with a turn-completed terminal event
