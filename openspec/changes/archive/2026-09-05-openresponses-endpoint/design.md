# Design: openresponses-endpoint

## Context

`detach-run-execution` lands first: runs survive consumer disconnects, the tap is droppable, and cancel is addressable. The facade is then a translator: parse an OpenResponses request, resolve agent/session, call `Runner.Run`/`Resume`, and project the `TranscriptEvent` stream onto the OpenResponses wire — SSE with `stream: true`, aggregated JSON otherwise. Workspace API keys do not exist yet (the only "API key" concept today is provider credentials), so the credential model is built here. The web app (React/Vite under `web/src`) adopts the OpenAI SDK against `/v1` for chat turns while keeping native endpoints for history catch-up, agent CRUD, and settings.

## Decisions

### D1 — Key is the tenant (chosen: API keys carry workspace scope; no workspace parameter)

A workspace API key resolves to (workspace, creator user). Every `/v1` operation scopes through it; there is no workspace slug/ID in the request. Cross-tenant references (session, agent slug) fail with not-found, indistinguishable from unknown. An org-key + `/w/:slug/v1` subpath scheme is reserved as a future routing change — no contract change needed for it, so nothing is built now.

Key shape: `oc_ws_<secret>`; stored as SHA-256 hash + prefix + last-4 for display; create returns plaintext exactly once. Table `workspace_api_keys(id, workspace_id, name, key_hash, key_prefix, key_suffix, created_by, created_at, revoked_at)`; lookup by hash on every `/v1` request (indexed, one query). The resolved creator user becomes `ExecRequest.UserID` (instruction composition renders `USER.md` from them).

### D2 — Session binding (chosen: metadata primary, chaining derived)

`metadata.onclaw_session` is the primary binding — the web app knows its sessions and passes them explicitly; unknown/foreign sessions are 404. `previous_response_id` is derived, not a parallel system: the facade mints `resp_<sessionID>_<turnID>` (both already opaque strings; a separator that cannot occur in either is chosen after checking their alphabets) and decodes it back. No new mapping table — the encoding is the mapping. Requests with neither run in a fresh ephemeral session: a session ID the store has never seen, so full-replay history is empty and persistence writes nothing durable (session-adapter writes for unknown sessions are suppressed this change; simplest is a throwaway session ID and letting events persist — decision: suppress, to honor the spec text "not persisted").

Ephemeral persistence suppression detail: `ExecRequest.SessionID` gains no new flag; the facade passes a `no-store` variant of the session adapter (records dropped, checkpoints skipped) constructed for the request. HITL cannot complete meaningfully in an ephemeral run (resume needs the checkpoint) — approval events still surface, but the spec documents resolution via the native path on bound sessions; ephemeral + approval is accepted as a degraded corner (documented, untested-against).

### D3 — Event translation (chosen: spec-native where faithful, `onclaw:` prefix where not)

| TranscriptEvent | Wire |
|---|---|
| `turn_started` | `response.created` + `response.in_progress` |
| `text_delta` | message item added (first delta) → `content_part.added` → `output_text.delta` ×N → `.done` → part done → item done |
| `reasoning_delta` | `onclaw:reasoning_delta` (custom; spec reasoning items need summary shapes we cannot fill faithfully) |
| `tool_call_started` | `output_item.added` — `function_call`, status `in_progress`, arguments carried |
| `tool_call_finished` | `output_item.done` (function_call, completed) + `onclaw:function_call_output` trace item (result, latency, is_error) |
| `message_completed` | assistant message `output_item.done` (dedup: skipped if the item was already closed by the delta path) |
| `context_compacted` | `onclaw:context_compacted` |
| `approval_required` | `onclaw:approval_required` then stream ends; response stays `incomplete` |
| `turn_completed` / `error` / `cancelled` | `response.completed` / `response.failed` / `response.incomplete` |

Output indices increment per emitted item; `sequence_number` per event, starting at 0. The aggregated (`stream: false`) path runs the same translation against the tap and folds items into `output`, dropping in-progress framing. Non-streaming waits for the terminal event — the request holds the connection for the turn's duration, which is the spec's own semantic for `stream: false`.

### D4 — Request constraints (chosen: reject multimodal, ignore instructions, intersect tools)

- `input`: string passthrough; item arrays flatten `input_text` in order; anything else → 400 naming the part. Multimodal is a later increment (the runner's input is text today).
- `instructions`: accepted, ignored — the composed instruction is the product (AGENTS/IDENTITY/SOUL/…). Documented in the endpoint spec so SDK users aren't surprised.
- `tools`: intersection with the agent allowlist (a request may narrow, never widen). `tool_choice: "none"` strips all tools for the turn. Other `tool_choice` values are accepted and not enforced.
- `temperature`/`max_output_tokens`/`max_tool_calls`: accepted, ignored this change (per-agent config governs; per-request overrides are a later decision with jail-safety implications).
- `store`: accepted; effectively always true for bound sessions, always false for ephemeral ones (D2).

### D5 — Usage capture (chosen: provider-reported totals on the terminal event)

The ADK model events carry provider usage; `drainAgentEvents` accumulates input/output/total across the turn and stamps the terminal `TranscriptEvent` (new `Usage *UsagePayload` field). Persisted via the session adapter's terminal record so history reloads can report it. The facade copies it into Response objects. No estimation — when a provider omits usage the field is omitted upstream and the Response reports zeros-absent.

### D6 — Error envelope (chosen: local mapping table, shared detection)

The existing `domain` error sentinels + handler error translation already classify native errors; the `/v1` translator maps them to spec types (`ErrInvalid` → 400 `invalid_request_error`, `ErrNotFound` → 404 `not_found_error`, conflict → 409 with `invalid_request_error` type + code, upstream model failure → `model_error`). Auth middleware rejects before handlers; key failures use 401 with `invalid_request_error` type and `invalid_api_key` code (spec has no 401 type; OpenAI precedent is `invalid_request_error`).

### D7 — Non-goals (rejected alternatives)

- WebSocket transport: SSE + polling covers the need; WS adds connection management with no bidirectional payoff (all client input is control-plane POSTs).
- `GET /v1/responses/{id}`: deferred until `store` semantics are exercised; the native `?after=` catch-up serves the web app.
- `/v1/responses/compact`: summarization is automatic mid-turn; no client-triggered compaction surface yet.
- Client-side tool execution: contradicts the jail/HITL model; server-side trace items are the wire representation.
