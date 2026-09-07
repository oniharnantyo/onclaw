# Tasks: openresponses-endpoint

## 1. Workspace API keys

- [x] 1.1 Migration (next number after 000017): `workspace_api_keys` table (id, workspace_id, name, key_hash, key_prefix, key_suffix, created_by, created_at, revoked_at) with up/down
- [x] 1.2 Domain + store port + Postgres adapter + test fake: key entity, `CreateKey`, `ListKeys`, `RevokeKey`, `LookupKeyByHash` (workspace-scoped reads for management, global lookup for authn)
- [x] 1.3 Key service: `oc_ws_` generation, SHA-256 hashing, prefix/suffix display; plaintext returned once
- [x] 1.4 Native endpoints: `GET/POST /api/workspaces/:ws/api-keys`, `DELETE /api/workspaces/:ws/api-keys/:id` — Owner/Admin permission-gated; create returns plaintext once
- [x] 1.5 `/v1` auth middleware: Bearer → hash lookup → (workspace, user) context; JWT rejected; failures use the OpenResponses error envelope (401, `invalid_api_key`)

## 2. Usage capture

- [x] 2.1 `UsagePayload{InputTokens, OutputTokens, TotalTokens}` on the terminal `TranscriptEvent`; `drainAgentEvents` accumulates from ADK model-event usage
- [x] 2.2 Session adapter: persist usage with the terminal record; `History` surfaces it on reloaded terminal events
- [x] 2.3 Tests: usage stamped on completed/error turns; history reload reports totals (fake model reporting usage)

## 3. Response identity & session binding

- [x] 3.1 `resp_<session>_<turn>` mint/decode helpers (separator chosen after checking session/turn ID alphabets; malformed IDs → invalid_request_error)
- [x] 3.2 Binding resolution: `metadata.onclaw_session` (must exist in key's workspace → 404 otherwise), `previous_response_id` decode, neither → ephemeral session with the no-store session adapter
- [x] 3.3 Ephemeral no-store adapter variant (drop records, skip checkpoints) with tests proving nothing persists

## 4. /v1/models

- [x] 4.1 `GET /v1/models` listing the key's workspace agents (id = slug); integration with the models catalog if display metadata is cheap, slugs-only otherwise

## 5. /v1/responses — parsing & validation

- [x] 5.1 Request DTOs per the OpenResponses surface (model, input, metadata, previous_response_id, tools, tool_choice, stream, instructions, temperature, max_output_tokens, store) with accepted-and-ignored fields documented
- [x] 5.2 Input flattening: string passthrough; `input_text` parts concatenated; unsupported parts → 400 naming the type
- [x] 5.3 `tools` intersection with the agent allowlist; `tool_choice: "none"` strips tools for the turn (ExecRequest/runner seam for per-turn narrowing)

## 6. /v1/responses — execution & translation

- [x] 6.1 Non-streaming path: `Run` → drain tap → fold items → Response JSON (status, output, usage, metadata echo)
- [x] 6.2 SSE translator: TranscriptEvent → spec events per D3 (created/in_progress, item lifecycle brackets, deltas, custom `onclaw:*` events, terminal response event, `[DONE]`, `sequence_number`)
- [x] 6.3 Approval short-circuit: `approval_required` → `onclaw:approval_required` + stream end, response `incomplete`; tests for the pause/resume-across-two-responses loop
- [x] 6.4 Error mapping per D6 (sentinels → envelope; model_error on upstream failure mid-stream → `response.failed`)

## 7. Web app adoption

- [x] 7.1 Add the OpenAI SDK client; chat turns move to `responses.create({model: agentSlug, stream: true, metadata: {onclaw_session}})` against `/v1`
- [x] 7.2 Chat transcript consumes the SSE event vocabulary (text deltas, tool-call cards incl. `onclaw:function_call_output` latency, `onclaw:approval_required` card → native approval endpoint)
- [x] 7.3 History catch-up and agent CRUD stay on native endpoints; API key management UI in workspace settings (create once / list / revoke)

## 8. Verification

- [x] 8.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 8.2 Integration suite (`-tags=integration`) against `DATABASE_URL` green — includes the api-keys migration round-trip
- [x] 8.3 Wire-level smoke: curl non-streaming + streaming turns, chained turn via `previous_response_id`, HITL pause → native approve → chained collection
- [ ] 8.4 `./scripts/smoke.sh` green; web app e2e (playwright) green with SDK-based chat
