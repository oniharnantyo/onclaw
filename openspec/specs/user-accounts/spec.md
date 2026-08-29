# user-accounts Specification

## Purpose

Identity for OnClaw: account lifecycle (creation, login, profiles, avatars) with login built on a pluggable provider registry so future SSO providers slot in without changing the login contract.

## Requirements

### Requirement: Password login
The system SHALL authenticate users with email + password and issue a session token on success. All failure modes SHALL return 401 with an identical error body: unknown email, wrong password, account without a password set, disabled account.

#### Scenario: Successful login
- **WHEN** a user submits correct credentials for an enabled account whose password is set
- **THEN** response is 200 with a session token and the user object

#### Scenario: Failure modes are indistinguishable
- **WHEN** login is attempted with an unknown email, a wrong password, a password-less account, or a disabled account
- **THEN** every failure returns 401 with an identical error body

### Requirement: Pluggable login providers
The login contract SHALL accept a `provider` field defaulting to `password`. An unregistered provider SHALL be rejected with 400. Future providers SHALL be registerable without changing the login contract.

#### Scenario: Provider defaults to password
- **WHEN** the login request omits `provider`
- **THEN** password authentication is used

#### Scenario: Unknown provider rejected
- **WHEN** the login request names an unregistered provider
- **THEN** response is 400 invalid_request and no token is issued

### Requirement: Authenticated requests
Protected endpoints SHALL require a valid bearer token. Invalid or expired tokens SHALL return 401. Disabled accounts SHALL be rejected even with a valid unexpired token.

#### Scenario: Valid token accepted
- **WHEN** a request carries a valid token of an enabled account
- **THEN** the handler runs with that user bound

#### Scenario: Disabled rejected despite valid token
- **WHEN** a disabled account presents a token issued before it was disabled
- **THEN** all authenticated requests return 401

#### Scenario: Expired token rejected
- **WHEN** a request carries a token past its expiry
- **THEN** response is 401

### Requirement: Session issuance configuration
Tokens SHALL have a configurable TTL (default 24h). With no configured signing secret, an ephemeral secret SHALL be generated and a warning SHALL be logged that restarts invalidate all sessions.

#### Scenario: Ephemeral secret
- **WHEN** the server starts with no configured signing secret
- **THEN** tokens issued before a restart stop authenticating after the restart

#### Scenario: TTL expiry
- **WHEN** a token is presented after its TTL elapses
- **THEN** authenticated requests return 401

### Requirement: Current user + switcher payload
`GET /auth/me` SHALL return the current user and their memberships, each with its role — the workspace switcher's data source.

#### Scenario: Me returns memberships
- **WHEN** a member of two workspaces calls GET /auth/me
- **THEN** response includes the user and both memberships, each with its role

### Requirement: Self profile update
`PATCH /users/me` SHALL allow a user to update their own name and to clear their avatar. Invalid values SHALL be rejected with 400.

#### Scenario: Rename
- **WHEN** a user sets a non-empty new name via PATCH /users/me
- **THEN** response is 200 with the updated user

#### Scenario: Invalid name rejected
- **WHEN** PATCH /users/me carries an empty name
- **THEN** response is 400 and nothing changes

#### Scenario: Clear avatar
- **WHEN** PATCH /users/me sets avatar to null
- **THEN** the stored avatar binding is removed and rendering falls back to initials

### Requirement: Avatar upload
`POST /users/me/avatar` SHALL accept a multipart file up to 2 MB of type PNG/JPEG/WebP verified by content sniffing, not by client-declared type. If the database update fails after the file is stored, the stored file SHALL be removed.

#### Scenario: Happy path
- **WHEN** a valid PNG under 2 MB is uploaded
- **THEN** it is stored, bound to the user, and the response carries a rendering URL

#### Scenario: Oversize rejected
- **WHEN** the upload exceeds 2 MB
- **THEN** response is 413 and nothing is stored or changed

#### Scenario: Spoofed type rejected
- **WHEN** non-image bytes are uploaded declared as an image
- **THEN** response is 400 and nothing is stored or changed

### Requirement: Avatar serving
Avatars SHALL be served without authentication via unguessable capability URLs. Unknown names SHALL 404.

#### Scenario: Serve avatar
- **WHEN** a stored avatar name is requested
- **THEN** stored bytes are served with the stored content type and immutable cache headers

#### Scenario: Unknown avatar
- **WHEN** an unknown name is requested
- **THEN** response is 404

### Requirement: CLI user management
`onclaw user create|list|disable` SHALL manage accounts without the web API. Disabled accounts SHALL be rejected at login.

#### Scenario: Create then login
- **WHEN** a user is created via CLI with a password
- **THEN** that user can log in through the API

#### Scenario: Disable blocks login
- **WHEN** a user is disabled via CLI
- **THEN** login returns 401 with the uniform error body

### Requirement: No public registration
There SHALL be no public account-creation HTTP endpoint; accounts come from instance seeding, the CLI, or admin direct-add.

#### Scenario: Register route absent
- **WHEN** any register route is requested
- **THEN** response is 404
