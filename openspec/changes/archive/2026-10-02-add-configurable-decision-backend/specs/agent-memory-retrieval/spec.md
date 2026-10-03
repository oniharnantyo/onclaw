# Spec Delta

## MODIFIED Requirements

### Requirement: Intent gate
Before composing context for a turn, the system SHALL run a bounded intent-gate classification (hard timeout, failing open) that decides whether the turn needs deep memory and, if so, which memory buckets are relevant. The gate's time budget SHALL be configurable per workspace through the memory settings record (`gate_budget_ms`, bounded to a sane range, absence = default) so deployments whose side-call model is a remote provider can afford the round trip; the shipped default SHALL be raised from 1500ms to 4000ms. A self-contained turn SHALL proceed with the always-injected documents only. A gate timeout or error SHALL be equivalent to "self-contained" — the gate is an optimization and never a correctness dependency, because the agent can always search explicitly.

The classification source SHALL be configurable per workspace through the memory settings record's decision configuration: absent, the gate classifies through the cheap LLM side-call exactly as before (the default); present, the gate classifies through the decision backend (the workspace's configured decision provider and model) under the same budget and the same fail-open contract. A decision-backend verdict SHALL route at most the notes and events buckets — the associative route and entity naming SHALL remain LLM-backend capabilities. Whatever the source, the verdict feeds the same prefetch path unchanged.

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

#### Scenario: Decision backend classifies the turn
- **WHEN** a workspace with a stored decision configuration runs a turn that needs deep memory
- **THEN** the classification runs against the decision backend inside the same budget, and a memory-needing verdict prefetches exactly as an LLM verdict would

#### Scenario: Decision backend never routes associative
- **WHEN** a turn about a named entity is classified through the decision backend
- **THEN** the verdict routes at most notes and events — no entity-graph traversal seeds the prefetch
