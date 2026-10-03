# Spec Delta

## Purpose

The workspace-configurable decision backend for the memory intent gate: a TypeSafe-compatible typed-decision endpoint that classifies each turn's memory need in one call, replacing the LLM side-call when configured, with the LLM path unchanged as the default.

## ADDED Requirements

### Requirement: Workspace decision configuration
The memory settings record SHALL carry an optional decision configuration: `decision_provider_id` and `decision_model`. Absence of the pair SHALL mean the intent gate classifies through the LLM side-call exactly as before — absence-is-defaults. The pair SHALL be stored together or not at all: a save that supplies exactly one of the two SHALL be rejected with a validation error. The referenced provider SHALL exist in the workspace and SHALL be of a decision-capable provider type (`typesafe`); a save referencing a language-model-only provider or an unknown provider SHALL be rejected. The model SHALL default to `jev-latest` in the UI; the server SHALL accept any non-empty model string. No schema migration SHALL be required — the fields ride the existing memory settings record.

#### Scenario: Decision backend configured
- **WHEN** an Owner saves the memory settings with `decision_provider_id` naming a `typesafe` provider config and `decision_model` "jev-latest"
- **THEN** the pair persists on the memory settings record and subsequent turns classify through the decision backend

#### Scenario: Half-set configuration rejected
- **WHEN** the memory settings are saved with `decision_provider_id` set but `decision_model` empty (or the reverse)
- **THEN** the save is rejected with a validation error and the stored configuration is unchanged

#### Scenario: Non-decision provider rejected
- **WHEN** the decision configuration references a provider config of type `openai`
- **THEN** the save is rejected with a validation error naming the provider-type constraint

#### Scenario: Absence keeps the LLM path
- **WHEN** the memory settings record carries no decision configuration
- **THEN** the intent gate classifies through the LLM side-call and no decision request is issued

### Requirement: TypeSafe decision call
When a decision configuration is stored, the intent gate SHALL classify the turn with one `POST /v1/systemone` request to the referenced provider's canonical origin (or its `base_url` override), authenticated with the provider's stored key: `state` is the raw turn text, `model` is the configured decision model, and `questions` carries exactly three noul questions — `needs_memory` ("does answering this turn require the workspace's long-term memory"), `bucket_notes` ("are durable stored facts relevant"), and `bucket_events` ("is what-happened-and-when relevant") — each with instructions and true/false criteria expressing the intent-gate policy. A noul probability at or above the threshold (default 0.5) SHALL be read as yes; `needs_memory` below threshold SHALL skip retrieval entirely; `needs_memory` above threshold with every bucket below threshold SHALL route both notes and events (the same narrowing-is-not-our-job rule as the LLM gate). The classification SHALL NOT issue more than one HTTP request per turn.

#### Scenario: Self-contained turn via decider
- **WHEN** the decider returns `needs_memory` below the threshold
- **THEN** no retrieval runs and the turn proceeds with the injected documents alone

#### Scenario: Needs memory with no bucket above threshold
- **WHEN** `needs_memory` is above the threshold but `bucket_notes` and `bucket_events` are both below it
- **THEN** the verdict routes both notes and events rather than nothing

#### Scenario: Routed buckets drive prefetch
- **WHEN** `needs_memory`, `bucket_notes`, and `bucket_events` are all above the threshold
- **THEN** the prefetch draws from the notes and events channels exactly as an LLM verdict routing both would

### Requirement: Decision budget and fail-open
The decision call SHALL run as a single attempt — no retry on any status — under the intent gate's existing hard budget (`gate_budget_ms`), with the deadline enforced locally so a slow endpoint is abandoned at the budget. Any failure — transport error, timeout, non-2xx status, malformed or undecodable answer body — SHALL be equivalent to "self-contained": the turn proceeds without retrieval, the failure is logged at warn, and nothing surfaces to the turn. The decider is an optimization and never a correctness dependency, exactly as the LLM gate is.

#### Scenario: Budget timeout abandons the call
- **WHEN** the decision endpoint does not answer within `gate_budget_ms`
- **THEN** the request is abandoned, the failure is logged at warn, and the turn proceeds without retrieval

#### Scenario: Auth failure fails open without retry
- **WHEN** the decision endpoint answers 401 for the stored key
- **THEN** the turn proceeds without retrieval in the same request budget, with no second attempt

#### Scenario: Malformed answer fails open
- **WHEN** the decision endpoint answers 200 with a body missing the `answers` map
- **THEN** the verdict is treated as self-contained and the turn proceeds

### Requirement: Associative unavailable in decision mode
The decision request SHALL NOT carry an associative question, and a decision-backend verdict SHALL never route the associative bucket or name an entity: entity-associative routing (graph traversal seeded by a named entity) SHALL remain an LLM-backend capability. A workspace wanting entity routing SHALL use the LLM side-call backend.

#### Scenario: Decision verdict routes at most notes and events
- **WHEN** the intent gate classifies through the decision backend
- **THEN** the prefetch draws from the notes and events channels only, never from entity-graph traversal
