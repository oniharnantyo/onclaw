## MODIFIED Requirements

### Requirement: API key authentication
The `/v1` surface SHALL authenticate requests with workspace API keys only. A valid key SHALL resolve to exactly one workspace and its creating user; the key's workspace SHALL be the tenant scope for every `/v1` operation. Keys SHALL be presented as `Authorization: Bearer <key>`, SHALL be stored hashed, and SHALL be creatable and revocable by workspace members through the native workspace settings surface. Key management SHALL be creator-symmetric: the creating member SHALL be able to list and revoke their own keys through the settings surface, while listing every workspace key and revoking another member's key SHALL require `workspace.write`. Session JWTs SHALL NOT authenticate `/v1` requests.

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

#### Scenario: Member manages own keys
- **WHEN** a Member who exchanged a key lists API keys through workspace settings, or revokes a key they created
- **THEN** the listing contains their own keys and their revoke succeeds — without workspace.write

#### Scenario: Member cannot manage others' keys
- **WHEN** a Member attempts to revoke a key created by another member
- **THEN** response is 403 (revoking others' keys requires workspace.write)

### Requirement: Session binding and chaining
A request SHALL bind to a session either by `metadata.onclaw_session` or by `previous_response_id` (an opaque response ID minted by the server that resolves to a prior response's session and turn). A `metadata.onclaw_session` naming a session with no persisted events in the key's workspace SHALL birth that session: the turn executes in a persistent session under the client-chosen ID, scoped to the key's workspace and owned by the key's creating user, and its history is persisted. Binding to an EXISTING session via `metadata.onclaw_session` SHALL be owner-scoped: the session SHALL resolve only when it was created by the key's creating user; another user's session, and sessions born from system execution (channels, schedulers, heartbeats), SHALL fail not-found indistinguishable from a foreign workspace's session. `previous_response_id` SHALL remain bind-only within the creating user's own sessions: a malformed ID SHALL fail with `invalid_request_error`, and an ID resolving to a session with no persisted events in the key's workspace — or to a session the key's creating user does not own — SHALL fail with not-found indistinguishable from a foreign workspace's session; it SHALL NEVER birth a session. A request with neither binding SHALL run in a fresh ephemeral session whose history is not persisted. Chained and metadata-bound requests append to the bound session's full-replay history.

#### Scenario: Metadata binding
- **WHEN** a request carries `metadata.onclaw_session` for an existing session created by the key's creating user
- **THEN** the turn appends to that session's history

#### Scenario: Session birth on first use
- **WHEN** a request carries `metadata.onclaw_session` naming a session with no persisted events in the key's workspace
- **THEN** the session is born under that ID in the key's workspace, owned by the key's creating user, the turn persists to it, and subsequent requests binding the same ID append to the same history

#### Scenario: Chained turn
- **WHEN** a request carries `previous_response_id` minted by an earlier response of the same key's creating user
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

#### Scenario: Foreign-user session rejected
- **WHEN** a request binds (by metadata or by previous_response_id) an existing session created by a different user of the same workspace
- **THEN** the request fails with not-found, indistinguishable from an unknown session

#### Scenario: System session rejected
- **WHEN** a request binds the session of a channel, scheduler, or heartbeat execution
- **THEN** the request fails with not-found; system-born sessions are not addressable by user API keys
