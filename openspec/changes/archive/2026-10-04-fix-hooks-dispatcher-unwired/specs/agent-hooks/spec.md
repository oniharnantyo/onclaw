# Spec Delta — agent-hooks

## ADDED Requirements

### Requirement: Runtime wiring

The runner that serves live turns SHALL resolve its hook chain from a **wired dispatcher** — one constructed over the hooks store and the shared handler registry — never from the silent no-op default. There SHALL be exactly one runner construction site in the shipping composition path, so the wiring cannot drift out of the option list that production uses. A hook that is stored, enabled, and interpretable SHALL gate the runs of its scope: a `pre_tool_use` block SHALL surface the canonical block payload as the tool result of the live call and SHALL be recorded in the hooks-execution audit. A deployment whose serving runner carries the no-op dispatcher SHALL fail this requirement even though every other hook surface (settings, validation, dry-run, audit API) reports healthy.

#### Scenario: Stored workspace hook gates a live /v1 turn

- **WHEN** a workspace stores an enabled `pre_tool_use` command hook matching a built-in tool, and a `/v1` turn calls that tool through the serving runner
- **THEN** the tool result is the canonical `{"blocked_by_hook":true,...}` payload, the tool's side effect does not occur, and the hooks-execution audit records the delivery with the block decision

#### Scenario: Composition without the dispatcher is detected, not silent

- **WHEN** the composition root assembles the runtime
- **THEN** the serving runner is constructed with the hooks dispatcher wired from the same store and registry the hook APIs use, and an integration test booting through the production composition path fails if the chain for a stored hook resolves empty
