# Spec Delta — agent-runtime

## MODIFIED Requirements

### Requirement: Instruction composition
At execution start, the system instruction SHALL be composed in fixed order from: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md`, `USER.md`, `CHANNEL.md`. The L1 base prompt (`AGENTS.md`) SHALL be injected from the platform-embedded template at every composition — it SHALL NOT be read from the agent's workspace directory, so template updates reach every agent on the next execution regardless of workspace age. `IDENTITY.md` and `SOUL.md` SHALL be read from the agent's workspace directory. `WORKSPACE.md` SHALL be rendered from the workspace record (name, description) plus a `## Shared memory` subsection carrying the workspace's shared memory content. `USER.md` SHALL be rendered from the calling user's record and workspace membership (name, email, role) plus a `## Memory` subsection carrying that user's own memory content. `CHANNEL.md` SHALL be rendered only when the execution carries channel context — from the channel record (name, slug, purpose, conventions), the member roster with specialization notes, and the channel catch-up tail as specified in the `agent-channels` capability — and SHALL occupy its fixed position after `USER.md` at the end of the workspace-document tier; executions without channel context SHALL omit it entirely. The memory subsections are distinct from the structured metadata (which remains free context): they carry preferences and information the structured fields do not capture. Composition SHALL happen per execution because `USER.md` varies by caller and memory may have changed since the previous turn, and missing documents and empty memory SHALL be skipped without failing the run (an empty memory omits its subsection entirely). The composed instruction SHALL carry a rich-cards guidance section — the markdown fence conventions for rendering card elements, with one shape per fence tag together with its rendering purpose and when-to-use triggers, example fences for the raw-source exception tags, a markdown-first clause steering tabular data to native markdown tables, and a cross-tag selection rule with anti-pattern guidance, plus the composition-tree (`ui`) conventions: the node protocol, the composition vocabulary, the constraint ranges, and the selection rule that composition is reserved for responses whose arrangement carries meaning — in attended, scheduler, and heartbeat compositions, positioned with the `AGENTS.md` base prompt.

#### Scenario: Fixed document order
- **WHEN** an execution composes its instruction with all six documents present
- **THEN** the system instruction contains their contents in the order AGENTS, IDENTITY, SOUL, WORKSPACE, USER, CHANNEL

#### Scenario: Base prompt always current
- **WHEN** the platform-embedded L1 template changes and an agent whose workspace was created before the change executes a turn
- **THEN** the composed instruction carries the new template content, not the agent's historically seeded copy

#### Scenario: Rich cards guidance in every profile
- **WHEN** an attended chat turn, a scheduler execution, and a heartbeat tick each compose their instruction
- **THEN** each composed instruction contains the rich-cards fence conventions

#### Scenario: USER.md varies by caller
- **WHEN** two different members execute the same agent
- **THEN** each execution's instruction carries that member's own name, email, and workspace role

#### Scenario: Missing documents tolerated
- **WHEN** an agent's prompt generation failed and IDENTITY.md/SOUL.md are absent
- **THEN** the execution still runs with the remaining documents (at minimum the injected base prompt plus the two virtual documents)

#### Scenario: Memory rides every turn
- **WHEN** an agent appends to `WORKSPACE.md` during one turn and executes a second turn
- **THEN** the second turn's composed instruction already contains the appended text under the `## Shared memory` subsection

#### Scenario: Empty memory omitted
- **WHEN** a user and workspace have no stored memory
- **THEN** the composed instruction carries the metadata docs without any memory subsections

#### Scenario: Channel execution gains CHANNEL.md
- **WHEN** an agent is summoned from a channel
- **THEN** the composed instruction contains CHANNEL.md — roster, specializations, conventions, catch-up tail — after USER.md, at the end of the workspace-document tier

#### Scenario: Non-channel execution unchanged
- **WHEN** an agent is executed through a direct chat
- **THEN** the composed instruction contains no CHANNEL.md section

#### Scenario: Composition guidance travels with the fence catalog
- **WHEN** any composition profile composes its instruction
- **THEN** the rich-cards section includes the `ui` tree conventions, the component vocabulary with its constraint ranges, and the rule that a single card is rendered by its own tag rather than a composition

#### Scenario: Rich-cards guidance carries per-tag triggers
- **WHEN** any composition (attended, scheduler, heartbeat) renders the base prompt
- **THEN** its rich-cards section pairs each fence tag's shape with rendering purpose and when-to-use triggers, exemplifies the raw-source exception tags, and steers tabular data to native markdown tables

