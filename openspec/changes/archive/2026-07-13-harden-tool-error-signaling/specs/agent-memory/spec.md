## ADDED Requirements

### Requirement: The memory tool's decline conditions are recoverable observations

The `memory` tool SHALL return each of its decline conditions as a tool-result observation with no
fatal error, so the agent turn continues and the model can correct the request. The decline
conditions are: a `replace`/`remove` operation whose `target` is absent from `MEMORY.md`; a
`target` that is not unique; an unknown `op`; and a write that would exceed the curated-core
character limit. The character-limit observation SHALL include guidance to consolidate or remove old
entries first.

#### Scenario: A missing replace/remove target is an observation

- **WHEN** the agent calls `memory` with `op` `replace`/`remove` and a `target` not present in
  `MEMORY.md`
- **THEN** the tool returns a human-readable observation that the target was not found, makes no
  change, and returns no fatal error so the agent turn continues

#### Scenario: A non-unique target is an observation

- **WHEN** the agent calls `memory` with a `target` that matches more than one location in
  `MEMORY.md`
- **THEN** the tool returns a human-readable observation that the target is not unique, makes no
  change, and returns no fatal error so the agent turn continues

#### Scenario: An unknown operation is an observation

- **WHEN** the agent calls `memory` with an `op` other than `add`, `replace`, or `remove`
- **THEN** the tool returns a human-readable observation naming the valid operations with no fatal
  error, and the agent turn continues

#### Scenario: A character-limit breach is an observation with guidance

- **WHEN** the agent calls `memory` with a write that would exceed the curated-core character limit
- **THEN** the tool returns a human-readable observation stating the limit and the guidance to
  consolidate or remove old entries, makes no change, and returns no fatal error so the agent turn
  continues

#### Scenario: An empty required knowledge-graph field is an observation

- **WHEN** the agent calls `kg_search` with an empty `seed_entity_name`
- **THEN** the tool returns a human-readable observation that `seed_entity_name` is required with no
  fatal error, and the agent turn continues
