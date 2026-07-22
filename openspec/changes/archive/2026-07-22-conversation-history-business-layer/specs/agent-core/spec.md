## ADDED Requirements

### Requirement: Agent.Run receives the assembled message list

`Agent.Run` SHALL accept the assembled conversation message list (`[]*schema.AgenticMessage`) as its input, so the business layer owns history assembly and the runner is stateless about storage. `Agent.Run` SHALL NOT accept only a new user-input string and SHALL NOT load or inject conversation history itself. System instruction and middleware-injected system context (e.g. curated memory) remain framework concerns applied during the run.

#### Scenario: The caller supplies history plus the new message

- **WHEN** an entrypoint starts a turn
- **THEN** it calls `Agent.Run` with the replayed history concatenated with the new user message
- **AND** the runner processes exactly that list without loading history

### Requirement: A framework turn collector hands the turn to an injected committer

A framework turn collector SHALL accumulate the turn's new messages from agent state (skipping system messages and already-persisted replayed messages) and, at turn end, hand the complete turn to an injected business-layer committer. The collector SHALL NOT hold the conversation store or write turn rows; it SHALL also emit per-call token usage to the registered event sink for live telemetry.

#### Scenario: The collector delegates persistence to the committer

- **WHEN** a turn reaches its end hook
- **THEN** the framework collector hands the accumulated turn messages to the injected committer
- **AND** the collector itself performs no store write