# openresponses Delta — web-live-chat-sessions

## MODIFIED Requirements

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

## ADDED Requirements

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
