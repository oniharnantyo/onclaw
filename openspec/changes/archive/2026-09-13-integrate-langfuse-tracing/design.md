## Context

Every agent turn already flows through the eino-based runner with full coordinates on `ExecRequest` (workspace, agent, session, user, origin) and persists transcript events; the runs table exists per scheduler-run surfaces. Eino's `callbacks.Handler` interface is the observability seam, and `eino-ext/callbacks/langfuse` implements it with trace/span/generation support, streaming capture, batching (FlushAt 15 / 500 ms), sampling, retries, and a `MaskFunc`. See proposal.md — Why.

Repo constraints: injected dependencies are never nil (optional capabilities are wired as absent registrations, not nil-guarded services); the composition root in `internal/cli` resolves everything; secrets follow the existing encrypted-credential patterns.

## Goals / Non-Goals

**Goals:**
- Turn-level Langfuse export with zero impact when unconfigured.
- Full attribution (session, user, workspace, agent, origin) using existing run coordinates.
- Secrets never leave the instance unmasked.
- Deep-linkable runs (persisted trace id).

**Non-Goals:**
- No custom traces UI — Langfuse is the observability surface; OnClaw only deep-links.
- No project-per-workspace tenancy in v1 (single backend project; workspace is metadata). Revisit if tenant isolation demands it.
- No export of approvals, hook blocks, compaction, or cancel reasons — those live in `session_events`; Langfuse sees the model/tool trajectory.
- No OTLP/alternative backends (Logfire, Jaeger) — the eino OTLP callback is a future follow-on.
- No Telegram-gateway coupling: gateway turns are traced automatically because they flow through the same runner; the gateway sets no trace context of its own.

## Decisions

### D1 — Callback handler registered at the composition root, absent when unconfigured
`internal/cli` builds the Langfuse handler from server config (`ONCLAW_LANGFUSE_*`) and passes it into the runner via a functional option (`WithTraceHandler`). When config is absent the option is not applied and the runner has no handler — no nil-guarded service, consistent with the injection rules. Alternative considered: always-constructed no-op handler — rejected; it adds a runtime dependency import path to every deployment for nothing.

### D2 — Trace context set per turn from ExecRequest
The runner applies `langfuse.SetTrace` on the run context before model composition: `WithSessionID(session_id)`, `WithUserID(user_id)`, `WithTags("origin:<origin>", "agent:<id>", "ws:<id>")`, `WithMetadata{turn_id, workspace_id, agent_id, origin}`. The trace name is the turn's input (first line, truncated) for interactive origins and the schedule name for scheduler fires. Trace = turn exactly — no session-level traces.

### D3 — Trace id capture and persistence
The handler wraps the callback so the generated Langfuse trace id is captured at export start and stamped onto the run record (existing run metadata, small migration if no JSON/metadata column exists on runs). The runs API passes `trace_id` and the configured host through to the web payload, so the UI composes the deep link without learning the host itself from client config.

### D4 — Masking is mandatory and centralized
`MaskFunc` redacts credential-shaped values (the existing secret-row patterns: web-search provider keys, gateway tokens, MCP credentials) and anything in hook secret payloads. Masking lives in `internal/observability` next to the handler construction so it cannot be bypassed by configuration.

### D5 — Sampling and failure semantics
`ONCLAW_LANGFUSE_SAMPLE_RATE` defaults to 1.0; sampling is deterministic per run id. The handler's bounded retries absorb transient backend failures; unreachable backends never fail a run — export is strictly best-effort after the turn's outcome is decided.

### D6 — Runs-view link-out
The run detail payload gains an optional `langfuse_url` (composed server-side from host + trace id). The runs view renders the action only when present — no client-side knowledge of Langfuse at all.

## Risks / Trade-offs

- **Coverage asymmetry**: approvals, hook decisions, and compaction are invisible in Langfuse — documented; the web transcript remains the complete record.
- **Single-project tenancy**: all workspaces share one Langfuse project with metadata separation — acceptable for self-hosted v1; project-per-workspace is the identified upgrade path.
- **Extra infra for operators**: Langfuse v3 brings its own datastore; strictly opt-in keeps unconfigured deployments untouched.
- **Streaming capture size**: long turns export large payloads; the handler's event-size cap truncates oversized fields.
