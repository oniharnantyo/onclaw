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
With `stream: true`, the endpoint SHALL emit `text/event-stream` with `data: <json>` frames, a terminal `data: [DONE]` line, and a monotonic integer `sequence_number` on every event. The stream SHALL follow the OpenResponses lifecycle: `response.created`, `response.in_progress`, per-item `output_item.added` / `output_item.done` with content-part and text-delta events bracketing assistant text, and exactly one terminal response event (`response.completed`, `response.failed`, or `response.incomplete`). Reasoning the model produces during generation SHALL stream as custom `onclaw:reasoning_delta` events, each carrying one incremental chunk of reasoning text, interleaved with text deltas and always before the terminal event. When a turn compacts the session's context — manually or by threshold — the stream SHALL emit a custom `onclaw:context_compacted` event carrying the working-context token estimates before and after compaction, before the terminal event. The same event SHALL be delivered by the catch-up attach replay of an in-flight run.

#### Scenario: Text turn stream
- **WHEN** an agent answers with text only
- **THEN** the stream contains created → in_progress → message item added → content part added → text deltas → part done → item done → completed → [DONE], with strictly increasing sequence numbers

#### Scenario: Reasoning turn stream
- **WHEN** the model produces reasoning before or during its answer
- **THEN** `onclaw:reasoning_delta` events carrying the reasoning chunks are interleaved with the stream before the terminal event, and a client that ignores unknown event types still receives a valid lifecycle

#### Scenario: Tool turn stream
- **WHEN** an agent invokes a tool
- **THEN** the stream emits the `function_call` item (added, then done with arguments) followed by the tool trace item (see "Server-side tool trace"), then continues

#### Scenario: Failed turn
- **WHEN** the run ends with the error transcript event
- **THEN** the stream terminates with `response.failed` carrying the error, then [DONE]

#### Scenario: Compaction event on the stream
- **WHEN** a turn compacts the session context, manually or at threshold
- **THEN** `onclaw:context_compacted` carrying the before/after token estimates arrives before the terminal event, and a client that ignores unknown event types still receives a valid lifecycle

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

### Requirement: Compact command
A request MAY set `metadata.onclaw_command` to `compact` to compact the bound session's context instead of running a normal model turn. The command SHALL require a session binding that resolves to persisted events in the key's workspace: an unknown or foreign `metadata.onclaw_session` SHALL fail not-found indistinguishable from a foreign workspace's session and SHALL NEVER birth a session; `previous_response_id` binds unchanged. The request's `input`, when non-empty, SHALL steer the summary generation as focus text and SHALL NOT be persisted as a user message. The turn SHALL stream `onclaw:context_compacted` (see Streaming event contract) when compaction occurs and SHALL terminate with `response.completed` carrying the summarizer call's usage and no output items; a bound session with no compactable history SHALL complete without the compaction event. Compact requests SHALL be subject to the same single-active-run conflict behavior as ordinary turns.

#### Scenario: Compact turn stream
- **WHEN** a compact-command request executes against a session with history
- **THEN** the stream contains `onclaw:context_compacted` carrying the token estimates, then `response.completed` with the summarizer usage and no output items, then [DONE]

#### Scenario: Focus text steers without persisting
- **WHEN** a compact request carries `input` "keep the API design decisions"
- **THEN** the summary is generated under that focus instruction and no user message is appended to the session

#### Scenario: Compact never births a session
- **WHEN** a compact request carries `metadata.onclaw_session` naming a session with no persisted events in the key's workspace
- **THEN** the request fails not-found and no session is created

#### Scenario: Quiet completion on empty history
- **WHEN** a compact request binds a session that exists but has no compactable message history
- **THEN** the response completes normally without emitting `onclaw:context_compacted`

#### Scenario: Conflict with an active run
- **WHEN** a compact request is submitted while another run is active on the same session
- **THEN** the same conflict behavior applies as for an ordinary turn

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
Tool calls SHALL execute server-side per the agent's allowlist; clients SHALL NOT feed tool outputs back. For each tool call the stream/response SHALL contain exactly one `function_call` output item carrying the call's arguments at `output_item.added`, followed by exactly one `output_item.done` for that same item carrying the same call's id and its complete arguments, then an `onclaw.function_call_output` output item delivered through the standard `response.output_item.added` / `response.output_item.done` events and present in the aggregated `response.output`, carrying the call ID, tool name, result payload, `latency_ms` when the call's duration was measured, and `is_error: true` when the call failed. Items SHALL be paired per call id: a call id already announced SHALL NOT be announced again, a `done` event SHALL belong to the call whose execution finished (never to a later-started call), and no `done` SHALL be dropped when multiple calls run in the same turn. Request-level `tools` SHALL be intersected with the agent's allowlist and SHALL never extend it; `tool_choice: "none"` SHALL run the turn without tools.

#### Scenario: Tool call trace
- **WHEN** an agent executes `web.fetch` during a turn
- **THEN** the output contains a `function_call` item with the call's arguments followed by an `onclaw.function_call_output` item with the fetched result, its measured latency, and the standard output-item added/done events

#### Scenario: Failed tool call trace
- **WHEN** a tool call fails during a turn
- **THEN** its `onclaw.function_call_output` item carries the error payload with `is_error: true`

#### Scenario: Parallel calls each get their own done
- **WHEN** an agent executes two tool calls issued by the same assistant message
- **THEN** the stream contains exactly one `function_call` `output_item.added` and exactly one `output_item.done` per call id, and each `done` carries its own call's id and complete arguments

#### Scenario: Re-announced call id is not duplicated
- **WHEN** the runtime reports a call's start twice for the same call id within one turn
- **THEN** the stream contains a single `function_call` `output_item.added` for that call id

#### Scenario: Request tools cannot extend the allowlist
- **WHEN** a request lists a tool the agent's allowlist excludes
- **THEN** the tool is not exposed for the turn

#### Scenario: Tool choice none
- **WHEN** a request sets `tool_choice: "none"`

### Requirement: Approval flow over the wire
When the run pauses for a dangerous shell-command approval, the stream SHALL emit the custom `onclaw:approval_required` event carrying the interrupt ID, command, and response/item identity, and SHALL end with [DONE] leaving the response `incomplete`. Resolution SHALL happen only through the native approval endpoint; public clients use the event payload's identifiers to route the decision out-of-band. After resolution the continued turn is a new response chained to the same session.

#### Scenario: Approval pauses the response
- **WHEN** the shell tool interrupts for approval during a streamed turn
- **THEN** the client receives `onclaw:approval_required` and then [DONE], with no terminal spec event

#### Scenario: Resolution continues as a new response
- **WHEN** the approval is resolved on the native endpoint
- **THEN** the resumed turn persists to the same session and is retrievable by chaining from the paused response's session

### Requirement: Usage reporting
Response objects (aggregated and terminal stream events) SHALL report token usage — input, output, and total — captured for the executed turn, plus the turn's final-call input tokens (the input count of the turn's last model call). The usage block MAY carry an optional `context_breakdown` object with labeled segment counts (`instructions`, `tools`, `conversation`, `files`, `server`) describing display-grade estimates of where the final call's input went; the field is additive and clients that ignore it remain fully functional. A turn whose provider reported no usage SHALL omit the `usage` block entirely rather than report zeros, and a breakdown SHALL NOT appear without a usage block.

#### Scenario: Usage on completed response
- **WHEN** a turn completes
- **THEN** the Response object carries `usage` with the turn's input/output/total token counts and the turn's final-call input token count

#### Scenario: Terminal events without a final answer still carry usage
- **WHEN** a turn ends as `response.incomplete` or `response.failed` after model calls were made
- **THEN** the Response object carries `usage` with the counts captured up to the terminal event

#### Scenario: No provider usage omits the block
- **WHEN** a turn's provider reports no usage for any of its model calls
- **THEN** the Response object carries no `usage` block

#### Scenario: Optional breakdown is additive
- **WHEN** a turn's usage carries a context breakdown, and a client reads only the legacy usage fields
- **THEN** the client behaves exactly as before, and the breakdown never appears without a usage block

### Requirement: OpenResponses error envelope
Errors on the `/v1` surface SHALL use the envelope `{error: {message, type, param, code}}` with the standard types: `invalid_request_error` (400), `not_found_error` (404), `rate_limit_error` (429), `model_error` (500, upstream model failure), `server_error` (500). Authentication and tenancy failures SHALL NOT leak workspace existence.

#### Scenario: Invalid request
- **WHEN** a request omits `model` or carries malformed input
- **THEN** the response is 400 with `invalid_request_error` naming the offending parameter

#### Scenario: Model failure mid-turn
- **WHEN** the upstream provider fails during a streamed turn
- **THEN** the stream terminates with `response.failed` and `error.type: "model_error"`

### Requirement: Multimodal input parts
The Responses endpoint SHALL accept image and file content parts in message input items, following the OpenResponses content-type schemas: `input_image` with an `image_url` (absolute URL or `data:` URL) and optional `detail`; `input_file` with a `file_url` and `filename`. A `file_url` MAY be an onclaw capability URL (resolved locally, never fetched over HTTP from itself) or an inline `data:` URL; `file_data` SHALL be tolerated as an alias for the inline file form. A `file_id` part SHALL be rejected with `invalid_param`. Remote third-party URLs SHALL be rejected `invalid_param` in v1 (fetching client-supplied remote URLs is out of scope). String-only and text-part-only inputs remain valid unchanged.

#### Scenario: Turn with image and text
- **WHEN** a request's user message contains an `input_text` part and an `input_image` part whose `image_url` is an onclaw capability URL
- **THEN** the turn executes with the image visible to the model, and the response streams exactly as a text-only turn does

#### Scenario: Inline data URL demoted to storage
- **WHEN** a request carries `input_image` with a `data:image/png;base64,…` URL
- **THEN** the server stores the bytes as an attachment in the key's workspace, replaces the inline form with a reference before persisting session events, and the turn proceeds identically to an uploaded attachment

#### Scenario: Text-like file inlined as fenced text
- **WHEN** a request carries `input_file` referencing a small text-like attachment
- **THEN** the turn message carries the file's content as a fenced text part rather than a file block

#### Scenario: OpenAI-style file_id rejected
- **WHEN** a request contains `input_file` with `file_id` only
- **THEN** the endpoint responds `400` `invalid_param` naming the unsupported `file_id` field

#### Scenario: Remote file URL rejected in v1
- **WHEN** a request contains `input_file` whose `file_url` is `https://example.com/doc.pdf`
- **THEN** the endpoint responds `400` `invalid_param` explaining that remote URLs are not accepted

#### Scenario: String-only input unchanged
- **WHEN** a request's input is a plain string or text-part array, as before this change
- **THEN** the turn executes exactly as it did before — no behavioral change for existing callers

### Requirement: Attachment resolution is workspace-scoped
Attachment references in input parts SHALL resolve against the API key's workspace before the turn executes; an unknown or foreign reference SHALL fail the request with `invalid_param` before any run starts.

#### Scenario: Unknown attachment id in URL path
- **WHEN** an input part references a capability URL whose key does not resolve to a stored attachment in the key's workspace
- **THEN** the request fails `400` `invalid_param` and no run is created

### Requirement: Compact turns reject attachments
Compact-command turns SHALL accept text input only; a compact request carrying image or file parts SHALL fail with `invalid_param` before resolution, consistent with compact's bind-only strictness.

#### Scenario: Compact with attachment rejected
- **WHEN** an `onclaw_command: "compact"` request carries an `input_image` part
- **THEN** the request fails `400` `invalid_param` stating compaction accepts text only, and the session is untouched
