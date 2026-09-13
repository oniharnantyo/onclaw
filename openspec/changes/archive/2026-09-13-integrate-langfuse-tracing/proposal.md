## Why

Agent turns are opaque in production: the web transcript shows the product view, but there is no span-level observability — raw model I/O, tool latency waterfalls, token/cost analytics, or cross-session search. Eino's callback system plus `eino-ext/callbacks/langfuse` exports exactly this to a self-hosted Langfuse instance, giving every origin (web, `/v1`, scheduler, Telegram) full turn-trajectory observability for a fraction of the effort of building a custom traces UI.

## What Changes

- New optional **Langfuse tracing export**: when a Langfuse backend is configured (host + public/secret keys via server config), every agent turn exports to Langfuse as **one trace** (a trace IS a turn — one execution), with model calls as generations and tool calls as spans.
- **Per-turn trace context** from existing run coordinates: session id → Langfuse session (native longitudinal grouping), member user id, and tags/metadata carrying workspace, agent, origin, and turn id.
- **Secret masking** on exported spans: tool config secrets and credential-shaped values are redacted before leaving the instance.
- **Cost controls**: batched async export with configurable sampling rate; absent configuration means zero tracing, zero added dependencies at runtime.
- **Trace-id persistence**: each run persists the Langfuse trace id so the product UI can deep-link.
- **Runs view link-out**: when tracing is configured and a run carries a trace id, the run's view gains an "Open in Langfuse" action.
- **BREAKING**: none. Tracing is strictly opt-in and additive; unconfigured deployments behave exactly as today.

## Capabilities

### New Capabilities
- `langfuse-tracing`: Optional turn-level trace export to a self-hosted Langfuse backend — trace-per-turn mapping, session/user/tag attribution, secret masking, sampling, trace-id persistence, and lifecycle (enabled only when configured).

### Modified Capabilities
- `web-app/runs`: The run view gains an "Open in Langfuse" link-out when the run carries a persisted trace id; absent a trace id (tracing disabled or pre-dating the export) the run view is unchanged.

## Impact

- **Code**: new `internal/observability/` (Langfuse callback wiring + trace-context helper); runner gains a per-turn trace-context hook and persists the trace id onto the run; composition-root wiring in `internal/cli`; a small addition to the run payload in `internal/server/handlers/agent_runs.go`.
- **Dependencies**: `github.com/cloudwego/eino-ext/callbacks/langfuse` (and its Langfuse Go client) — imported at the composition root only.
- **Config**: `ONCLAW_LANGFUSE_HOST`, `ONCLAW_LANGFUSE_PUBLIC_KEY`, `ONCLAW_LANGFUSE_SECRET_KEY`, `ONCLAW_LANGFUSE_SAMPLE_RATE` (all optional; none set = tracing disabled).
- **Schema**: trace id persisted on existing run/session records (small migration or reuse of existing run metadata columns, per design).
- **Ops**: self-hosted Langfuse (docker-compose) is the operator's deployment concern; OnClaw only needs network reachability and keys.
