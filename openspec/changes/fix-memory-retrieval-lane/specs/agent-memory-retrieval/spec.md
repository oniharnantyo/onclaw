# agent-memory-retrieval Delta

## MODIFIED Requirements

### Requirement: Intent gate
Before composing context for a turn, the system SHALL run a bounded intent-gate classification (cheap model, hard timeout, failing open) that decides whether the turn needs deep memory and, if so, which memory buckets are relevant. The gate's time budget SHALL be configurable per workspace through the memory settings record (`gate_budget_ms`, bounded to a sane range, absence = default) so deployments whose side-call model is a remote provider can afford the round trip; the shipped default SHALL be raised from 1500ms to 4000ms. A self-contained turn SHALL proceed with the always-injected documents only. A gate timeout or error SHALL be equivalent to "self-contained" — the gate is an optimization and never a correctness dependency, because the agent can always search explicitly.

#### Scenario: Self-contained turn skips retrieval
- **WHEN** a turn requires no workspace context per the gate
- **THEN** no retrieval runs and the turn proceeds with the injected documents alone

#### Scenario: Gate failure fails open
- **WHEN** the gate model times out or errors
- **THEN** the turn proceeds immediately with the injected documents alone and the agent can still search

#### Scenario: Workspace raises the gate budget
- **WHEN** a workspace whose side-call model is a remote provider saves `gate_budget_ms` within the allowed range
- **THEN** gate classification on subsequent turns is bounded by the configured budget instead of the default, and a classification that completes within it still prefetches

#### Scenario: Out-of-range budget is rejected
- **WHEN** the memory settings record is saved with `gate_budget_ms` outside the allowed range or of a non-integer type
- **THEN** the save is rejected with a validation error naming the allowed range, and the previously stored budget remains in force
