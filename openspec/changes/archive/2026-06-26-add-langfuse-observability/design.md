# Design — add-langfuse-observability

## Context

onclaw currently does not have external observability, making it difficult to debug multi-turn agent runs. We want to add opt-in Langfuse tracing.
Eino's callbacks mechanism allows globally appending handlers to trace all agent/tool executions automatically.

## Goals / Non-Goals

**Goals:**
- Add opt-in Langfuse tracing.
- Support secret masking before egressing logs.
- Flush trace events on shutdown.
- Ensure zero CGO dependency to keep binary size small and cross-compilable.

**Non-Goals:**
- Tracing other providers or non-agent commands.

## Decisions

### 1. Leaf Package for Observability Setup
We implement `internal/observability` as a leaf package which has no dependencies on `internal/config` or `internal/agent/tools`.
- *Rationale*: Prevents circular dependencies since the CLI command package depends on config, agent, and observability.
- *Alternatives*: Placing setup directly in CLI commands, which makes it less testable and violates separation of concerns.

### 2. Globally Registered Eino Callback Handler
We register the handler globally via `callbacks.AppendGlobalHandlers`.
- *Rationale*: Eino automatically fans out global handlers across all model and tool executions. This covers the entire agent ReAct loop automatically.

## Risks / Trade-offs

- [Risk] Network egress block/delay → [Mitigation] Best-effort async delivery and explicit `flush()` on teardown.
- [Risk] Secret leak to external service → [Mitigation] Enable masking by default using `tools.Redact` pattern matcher.
