# agent-memory-retrieval Specification

## Purpose
The read path for extracted memory: an intent gate that bounds retrieval cost, an agent-facing read-only search tool over the episodic and semantic stores, citation-locked grounding (only evidence actually opened is citable; otherwise abstain), and scope-filtered visibility so multi-tenant reads never cross privacy boundaries.

## ADDED Requirements

### Requirement: Intent gate
Before composing context for a turn, the system SHALL run a bounded intent-gate classification (cheap model, hard timeout, failing open) that decides whether the turn needs deep memory and, if so, which memory buckets are relevant. A self-contained turn SHALL proceed with the always-injected documents only. A gate timeout or error SHALL be equivalent to "self-contained" — the gate is an optimization and never a correctness dependency, because the agent can always search explicitly.

#### Scenario: Self-contained turn skips retrieval
- **WHEN** a turn requires no workspace context per the gate
- **THEN** no retrieval runs and the turn proceeds with the injected documents alone

#### Scenario: Gate failure fails open
- **WHEN** the gate model times out or errors
- **THEN** the turn proceeds immediately with the injected documents alone and the agent can still search

### Requirement: Prefetch injection
When the gate finds a turn needs deep memory, the system SHALL retrieve a small bounded set of candidates (top-k, hard cap on injected size) from the routed buckets and inject them as cited candidates alongside the always-injected documents. Retrieved candidates SHALL carry their evidence pointers and visibility stamps.

#### Scenario: Prefetch is bounded
- **WHEN** the gate routes a turn to retrieval
- **THEN** at most k candidates within a fixed token budget are injected, each carrying its evidence pointer

### Requirement: Memory search tool
Agents SHALL have exactly one read-only memory search tool exposing the notes and events stores with agent-settable filters (time window, visibility bucket, free-text query). Retrieval itself SHALL use hybrid lexical search (full-text plus trigram) and SHALL NOT require a model call. The tool SHALL be identity-bound to the executing run's workspace, user, and agent — no argument can widen what it searches. The tool SHALL NOT write under any circumstance.

#### Scenario: Agent searches on demand
- **WHEN** an agent calls the search tool with a query and a time-window filter
- **THEN** it receives matching notes and events with provenance, without any ingestion or model side-effect

#### Scenario: Search cannot cross identity
- **WHEN** an agent executing for one member searches memory
- **THEN** results exclude every other member's user-visibility rows regardless of query content

### Requirement: Citation lock and abstention
Answers SHALL only cite memory evidence that was actually opened during retrieval for that turn — a row merely existing in the store is not citable. When retrieval and search open nothing relevant, the agent's answer SHALL state that nothing is recorded rather than fabricating recalled content.

#### Scenario: Only opened evidence is cited
- **WHEN** an answer relies on stored memory
- **THEN** every memory-backed claim traces to an evidence pointer that was opened during this turn's retrieval

#### Scenario: Nothing found means nothing claimed
- **WHEN** a question has no matching memory and no opened evidence
- **THEN** the agent states nothing is recorded rather than guessing

### Requirement: Scope-filtered reads
Every memory read — prefetch, search, or direct load — SHALL compute the visible set structurally from the caller: workspace-shared rows, the caller's own user-visibility rows, and the serving agent's agent-visibility rows. Visibility filtering SHALL be enforced in the query layer, never delegated to prompts, and graph traversal (wave 3) SHALL inherit the same filtering through edge visibility.

#### Scenario: Member boundary holds under search
- **WHEN** Budi searches memory in a channel
- **THEN** the visible set is workspace-shared rows plus Budi's own user rows plus the serving agent's agent rows — never Sari's unpromoted facts

#### Scenario: Wave-3 traversal cannot leak
- **WHEN** graph retrieval walks from a shared entity toward linked events
- **THEN** edges into user-visibility events are excluded for callers who could not read those events directly
