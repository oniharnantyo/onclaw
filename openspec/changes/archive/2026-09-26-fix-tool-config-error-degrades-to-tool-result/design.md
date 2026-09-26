# Design

## Context

Tool construction and tool execution are two separate failure lanes. Execution errors are already graceful: `toolErrorResultMiddleware` (internal/agents/tool_error_result.go) converts every tool invocation error into a JSON error result the model reads, and the transcript renders error tool-call cards. Construction errors have no such net: `ResolvedTools` returns the first constructor error and the run dies before the model is called. Only `web.search` fails construction in reachable code — `searchProviderFor` raises `errWebSearchNotConfigured` (and unknown-provider / missing-credential errors) when the workspace has no usable provider entries — but the same shape exists latently in `channel.post`, `session.close` (defensively guarded, stripped at resolution) and the file-family constructors (agent-dir stat checks).

See proposal.md — Why for motivation.

## Goals / Non-Goals

Goals:
- A tool whose workspace configuration is missing or invalid must never fail the run; the error surfaces on the tool call, the agent reads it and answers another way.
- Keep the explicit, actionable error text and the no-credential-free-fallback rule; the error is still raised before any network attempt.
- Remove the latent agent-dir construction failure in the file-family tools by self-healing.

Non-Goals:
- No registry-wide "degraded stub" mechanism for constructor failures (see D1 — rejected).
- No change to the tools API enable gating: a workspace still cannot enable `web.search` without a fully valid entry; the degradation exists for enabled-by-default / env-fallback states.
- No proactive Settings → Tools warning when an agent exposes an unconfigured configurable tool (separate future thread).
- No change to `channel.post` / `session.close` guards (see D6).

## Decisions

### D1: Lazy provider resolution, not a stub, not a drop
`web.search` builds immediately with a resolver closure; the provider chain resolves on first invocation and the error (if any) flows through the existing error-result middleware as `{"error": ..., "tool": "web.search"}`.

- *Why not a registry-wide stub* (constructor failure → generic always-failing tool): the stub cannot carry the real parameter schema or description, so the model calls it blind; it converts hypothetical wiring bugs into quiet per-call errors; and it changes `ResolvedTools`' contract for a lane exactly one tool occupies today. If a second config-dependent tool appears, promoting to a registry-wide mechanism is a clean follow-up.
- *Why not silently dropping the unbuilt tool* (the schedule-tool/todo precedent — "no broken tools"): that precedent fits deployment-level wiring (a store not injected), where absence is permanent and global. A missing workspace provider is per-workspace, visible configuration state; dropping the tool hides the misconfiguration from both the model and the user and invites confidently unsearched answers. Visibility is the point.

### D2: Resolve once per execution, memoize the failure
The resolver closure runs on first `InvokableRun`, memoized with `sync.Once` on the tool instance (one instance per execution). A failed resolution caches the error, so repeat invocations in the same run return the identical error cheaply. Tool configs are loaded once per run (runner), so there is no mid-run configuration change to miss; per-execution memoization preserves today's chain-construction semantics exactly, only moved later.

### D3: Error surface is the existing one
No new event kind, payload, or frontend work: the middleware wraps the returned error into the standard error-result JSON, and the transcript already renders error tool-call cards. The error text stays `web.search is not configured — add a provider in Settings → Tools` (and the provider-naming variants for unknown provider / missing credential), so the model can relay the fix verbatim.

### D4: `web.search` stays on the model's tool surface when unconfigured
The tool remains advertised in the tool list. The model may invoke it and receive the error — that is the intended visibility. This is the deliberate opposite of the schedule-tool precedent (D1) for workspace-configuration gaps.

### D5: File-family constructors self-heal the agent dir
`files.delete`, `document.read`, `document.create` replace their `stat`-and-fail construction checks with `MkdirAll` of the agent workspace directory — mirroring the system-skills "disk is a self-healing cache" stance. A missing dir (ops accident, restored DB without files) heals instead of killing every run for that agent; a genuinely unwritable disk still fails construction loudly, which is correct. No spec delta: `workspace-document-tools` pins no construction behavior.

### D6: Channel/session construction guards stay
`channel.post` / `session.close` construction errors are unreachable (exposure is stripped at resolution before construction) and their nil-checks test optional run context, which the injected-dependencies nil-check policy explicitly permits. They are correct as written.

## Risks / Trade-offs

- [Model repeatedly invokes the unconfigured tool across turns] → The error result is cheap (memoized, no network) and the text directs the fix; acceptable cost of visibility. A model that never calls the tool produces no noise at all.
- [Agent-dir self-heal masks an ops-level anomaly (DB restored, files wiped)] → The heal is bounded to re-creating the directory; contents stay gone and the agent restarts from an empty workspace, which is the honest state. Permission failures still fail loudly.
- [Unconfigured `web.search` occupies tool-surface budget every turn] → One tool entry; negligible.
- [Test churn: `tool_registry_test.go` asserts the construction error today] → Expected flip; the assertion moves to the invocation lane.

## Migration Plan

No schema, API, or config migration. Deploy is a plain binary rollout: runs that previously died at tool resolution now proceed, and unconfigured `web.search` calls return error results. Rollback is the previous binary. The `agent-runtime` DuckDuckGo drift fix rides the delta archive.

## Open Questions

None.
