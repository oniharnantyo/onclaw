# Proposal: openresponses-endpoint

## Why

The web app and future public integrations need a standard way to drive agents. OpenResponses (the open spec built on the OpenAI Responses API) makes the agent reachable by the OpenAI client SDK — `baseURL` swap, `model: "<agent-slug>"` — and any Responses-compatible client. The runner already streams a domain event vocabulary and persists full-replay history; what is missing is the wire surface and the credential model to put in front of it. This change depends on `detach-run-execution` (runs must survive client disconnects before an SDK-facing endpoint can exist).

## What Changes

- **`POST /v1/responses`.** OpenResponses wire contract: `stream: false` returns the aggregated Response JSON; `stream: true` returns the semantic SSE event stream (`response.created` → item lifecycle → `response.completed`, terminal `data: [DONE]`, monotonic `sequence_number`). The agent is the model: `model` resolves an agent slug within the caller's workspace.
- **`GET /v1/models`.** Lists the workspace's agents as models (`id` = agent slug).
- **Workspace API keys.** New credential type for the `/v1` surface: per-workspace keys (`oc_ws_…`, stored hashed), created/revoked through workspace settings, resolving workspace + creator user per request. `/v1` accepts only API keys; the native `/api` surface keeps JWT.
- **Session binding.** `metadata.onclaw_session` names the OnClaw session (primary, used by the web app) and must reference an existing session of the key's workspace (404 otherwise); requests with no session binding run in a fresh ephemeral session with no persisted history. `previous_response_id` chaining works for spec-pure clients — response IDs are opaque `resp_<session>_<turn>` encodings that resolve to the same session.
- **Server-side tools over the wire.** Tool calls execute inside the run (agent allowlist governs); the stream emits the `function_call` item and an `onclaw:function_call_output` trace item carrying the result and latency. Request-level `tools` never extend the agent's allowlist (intersection, empty by default); `tool_choice: "none"` strips tools for the turn.
- **HITL over the wire.** `approval_required` emits the custom `onclaw:approval_required` event and ends the stream with the response left `incomplete`; resolution stays on the native approval endpoint. Public clients discover it via the event payload (interrupt ID + command).
- **Usage capture.** Token usage is captured per turn by the runner and reported in the Response object (`usage`); this closes the one runner-adjacent gap and is a prerequisite for any metered public exposure.
- **Error envelope.** `/v1` errors use the OpenResponses `{error: {message, type, param, code}}` shape with standard types.
- **Input handling.** `input` as a string passes through; item arrays flatten `input_text` parts to the turn input; other part types (images, files) are rejected `invalid_request_error` for now. `instructions` is accepted and ignored — the agent's composed instruction always wins.

## Capabilities

### New Capabilities

- `openresponses`: the `/v1` surface — API-key authn, models listing, responses endpoint, streaming contract, session binding, tool trace, approval flow, error envelope, usage reporting.

### Modified Capabilities

- `agent-runtime`: per-turn token usage capture (new requirement; feeds the Response `usage` field).

## Impact

- **New tables:** workspace API keys (one up/down migration pair).
- **Auth split:** `/v1` middleware authenticates API keys only (JWT rejected there); native surface unchanged. Keys carry workspace scope — the key is the tenant; no workspace parameter exists on `/v1` (an org-key/subpath scheme is a reserved future extension, not built here).
- **Runner:** usage plumbing (ADK model usage → per-turn totals) is the only runner-side change; the `/v1` handler is otherwise a pure translator over `Run`/`Resume` + `EventStream`, which `detach-run-execution` made detach-safe.
- **Not in scope:** WebSocket transport, `/v1/responses/compact`, `GET /v1/responses/{id}` (follow-up once `store` semantics are exercised), multimodal input, client-side tool execution.
