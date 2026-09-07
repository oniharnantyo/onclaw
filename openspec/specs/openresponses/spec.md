# openresponses Specification

## Purpose

A `/v1` surface compatible with the OpenResponses (OpenAI Responses API) contract, letting the web app and any OpenAI-SDK client drive workspace agents with API-key authentication, agent-as-model addressing, streamed Responses events, and server-side tool trace and approval events.

## Requirements

### Requirement: API key authentication
The `/v1` surface SHALL authenticate requests with workspace API keys only. A valid key SHALL resolve to exactly one workspace and its creating user; the key's workspace SHALL be the tenant scope for every `/v1` operation. Keys SHALL be presented as `Authorization: Bearer <key>`, SHALL be stored hashed, and SHALL be creatable and revocable by workspace members through the native workspace settings surface. Session JWTs SHALL NOT authenticate `/v1` requests.

#### Scenario: Key resolves tenant scope
- **WHEN** a request presents a valid workspace API key
- **THEN** all `/v1` operations on that request resolve agents and sessions within that key's workspace only

#### Scenario: Invalid or revoked key rejected
- **WHEN** a request presents an unknown, revoked, or malformed key
- **THEN** the request fails with the OpenResponses error envelope and an authentication error code

#### Scenario: JWT is not valid on /v1
- **WHEN** a request presents a valid session JWT to a `/v1` endpoint
- **THEN** the request is rejected as unauthenticated

#### Scenario: Key management on the native surface
- **WHEN** a workspace Owner or Admin creates or revokes an API key through workspace settings
- **THEN** the key is returned in plaintext once at creation, stored hashed, and revoked keys stop authenticating immediately

### Requirement: Agents as models
`GET /v1/models` SHALL list the workspace's agents as models, with the agent slug as the model `id`. On `POST /v1/responses`, the `model` field SHALL resolve to an agent slug within the key's workspace; an unknown or foreign slug SHALL fail with a `model_not_found` error.

#### Scenario: Models listing
- **WHEN** a client lists models
- **THEN** each workspace agent appears with its slug as the id

#### Scenario: Model resolution
- **WHEN** a response request names `model: "atlas"` and an agent with slug `atlas` exists in the key's workspace
- **THEN** the run executes that agent's configuration

#### Scenario: Unknown model
- **WHEN** a response request names a slug that is not an agent of the key's workspace
- **THEN** the request fails with `model_not_found`, indistinguishable from a foreign workspace's agent

### Requirement: Responses endpoint
`POST /v1/responses` SHALL execute one agent turn. With `stream: false` it SHALL return the aggregated Response object when the turn reaches a terminal state; with `stream: true` it SHALL return the OpenResponses SSE stream. The request's `input` SHALL become the turn input: a string passes through, an item array flattens its `input_text` parts in order, and unsupported part types (images, files, video) SHALL be rejected with `invalid_request_error`. The request field `instructions` SHALL be accepted and ignored — the agent's composed instruction governs.

#### Scenario: Non-streaming turn
- **WHEN** a request with `stream: false` executes to completion
- **THEN** the response is a single Response JSON object with status `completed` and the turn's output items

#### Scenario: Input flattening
- **WHEN** `input` is an array of message items carrying `input_text` parts
- **THEN** the parts are concatenated in order as the turn input

#### Scenario: Unsupported input part
- **WHEN** `input` contains an `input_image` or other unsupported part
- **THEN** the request fails with `invalid_request_error` naming the part type

### Requirement: Streaming event contract
With `stream: true`, the endpoint SHALL emit `text/event-stream` with `data: <json>` frames, a terminal `data: [DONE]` line, and a monotonic integer `sequence_number` on every event. The stream SHALL follow the OpenResponses lifecycle: `response.created`, `response.in_progress`, per-item `output_item.added` / `output_item.done` with content-part and text-delta events bracketing assistant text, and exactly one terminal response event (`response.completed`, `response.failed`, or `response.incomplete`).

#### Scenario: Text turn stream
- **WHEN** an agent answers with text only
- **THEN** the stream contains created → in_progress → message item added → content part added → text deltas → part done → item done → completed → [DONE], with strictly increasing sequence numbers

#### Scenario: Tool turn stream
- **WHEN** an agent invokes a tool
- **THEN** the stream emits the `function_call` item (added, then done with arguments) followed by the tool trace item (see "Server-side tool trace"), then continues

#### Scenario: Failed turn
- **WHEN** the run ends with the error transcript event
- **THEN** the stream terminates with `response.failed` carrying the error, then [DONE]

### Requirement: Session binding and chaining
A request SHALL bind to a session either by `metadata.onclaw_session` or by `previous_response_id` (an opaque response ID minted by the server that resolves to a prior response's session and turn). A `metadata.onclaw_session` naming a session with no persisted events in the key's workspace SHALL birth that session: the turn executes in a persistent session under the client-chosen ID, scoped to the key's workspace, and its history is persisted. `previous_response_id` SHALL remain bind-only: a malformed ID SHALL fail with `invalid_request_error`, and an ID resolving to a session with no persisted events in the key's workspace SHALL fail with not-found indistinguishable from a foreign workspace's session; it SHALL NEVER birth a session. A request with neither binding SHALL run in a fresh ephemeral session whose history is not persisted. Chained and metadata-bound requests append to the bound session's full-replay history.

#### Scenario: Metadata binding
- **WHEN** a request carries `metadata.onclaw_session` for an existing session of the workspace
- **THEN** the turn appends to that session's history

#### Scenario: Session birth on first use
- **WHEN** a request carries `metadata.onclaw_session` naming a session with no persisted events in the key's workspace
- **THEN** the session is born under that ID in the key's workspace, the turn persists to it, and subsequent requests binding the same ID append to the same history

#### Scenario: Chained turn
- **WHEN** a request carries `previous_response_id` minted by an earlier response
- **THEN** the turn appends to that response's session; the chain is valid only within the key's workspace

#### Scenario: Chaining cannot bootstrap
- **WHEN** a request carries a well-formed `previous_response_id` whose session has no persisted events in the key's workspace
- **THEN** the request fails with not-found and no session is created

#### Scenario: Unbound request
- **WHEN** a request carries no session metadata and no `previous_response_id`
- **THEN** the turn runs in an ephemeral session and its transcript is not persisted

#### Scenario: Cross-workspace isolation
- **WHEN** a request binds a session ID that another workspace's session already uses
- **THEN** the other workspace's session is untouched and unreadable; the request births or binds an independent session in the key's workspace only

#### Scenario: Foreign session rejected
- **WHEN** a chained request's `previous_response_id` resolves to a session that exists but belongs to another workspace
- **THEN** the request fails with not-found, indistinguishable from an unknown session

### Requirement: Session key exchange
An authenticated native endpoint SHALL mint a workspace-scoped API key for the authenticated user: the caller names the workspace, membership in that workspace SHALL be sufficient authorization, and the minted key SHALL behave identically to a settings-created key — returned in plaintext exactly once, stored hashed, scoped to the named workspace, revocable through workspace settings. The endpoint SHALL NOT require workspace-admin privileges.

#### Scenario: Member exchanges for a chat key
- **WHEN** an authenticated Member names a workspace they belong to
- **THEN** a new key scoped to that workspace is created, returned in plaintext exactly once, and authenticates `/v1` requests with that workspace as tenant scope

#### Scenario: Non-member rejected
- **WHEN** an authenticated user names a workspace they do not belong to
- **THEN** the request fails without revealing whether the workspace exists

#### Scenario: Unauthenticated rejected
- **WHEN** the endpoint is called without a valid session JWT
- **THEN** the request is rejected as unauthenticated

#### Scenario: Exchanged key is revocable
- **WHEN** a workspace Owner or Admin revokes an exchanged key through workspace settings
- **THEN** the key stops authenticating immediately, like any settings-created key

### Requirement: Server-side tool trace
Tool calls SHALL execute server-side per the agent's allowlist; clients SHALL NOT feed tool outputs back. For each tool call the stream/response SHALL contain the `function_call` output item with the call's arguments, followed by a custom `onclaw:function_call_output` output item carrying the call ID, tool name, result payload, and latency. Request-level `tools` SHALL be intersected with the agent's allowlist and SHALL never extend it; `tool_choice: "none"` SHALL run the turn without tools.

#### Scenario: Tool call trace
- **WHEN** an agent executes `web.fetch` during a turn
- **THEN** the output contains a `function_call` item for the call followed by `onclaw:function_call_output` with the fetched result and latency

#### Scenario: Request tools cannot extend the allowlist
- **WHEN** a request lists a tool the agent's allowlist excludes
- **THEN** the tool is not exposed for the turn

#### Scenario: Tool choice none
- **WHEN** a request sets `tool_choice: "none"`
- **THEN** the turn runs with no tools and the model cannot invoke any

### Requirement: Approval flow over the wire
When the run pauses for a dangerous shell-command approval, the stream SHALL emit the custom `onclaw:approval_required` event carrying the interrupt ID, command, and response/item identity, and SHALL end with [DONE] leaving the response `incomplete`. Resolution SHALL happen only through the native approval endpoint; public clients use the event payload's identifiers to route the decision out-of-band. After resolution the continued turn is a new response chained to the same session.

#### Scenario: Approval pauses the response
- **WHEN** the shell tool interrupts for approval during a streamed turn
- **THEN** the client receives `onclaw:approval_required` and then [DONE], with no terminal spec event

#### Scenario: Resolution continues as a new response
- **WHEN** the approval is resolved on the native endpoint
- **THEN** the resumed turn persists to the same session and is retrievable by chaining from the paused response's session

### Requirement: Usage reporting
Response objects (aggregated and terminal stream events) SHALL report token usage — input, output, and total — captured for the executed turn.

#### Scenario: Usage on completed response
- **WHEN** a turn completes
- **THEN** the Response object carries `usage` with the turn's input/output/total token counts

### Requirement: OpenResponses error envelope
Errors on the `/v1` surface SHALL use the envelope `{error: {message, type, param, code}}` with the standard types: `invalid_request_error` (400), `not_found_error` (404), `rate_limit_error` (429), `model_error` (500, upstream model failure), `server_error` (500). Authentication and tenancy failures SHALL NOT leak workspace existence.

#### Scenario: Invalid request
- **WHEN** a request omits `model` or carries malformed input
- **THEN** the response is 400 with `invalid_request_error` naming the offending parameter

#### Scenario: Model failure mid-turn
- **WHEN** the upstream provider fails during a streamed turn
- **THEN** the stream terminates with `response.failed` and `error.type: "model_error"`
