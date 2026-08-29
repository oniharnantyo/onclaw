## Purpose

Session layer for the web app: the login screen, session boot and hydration, 401 handling, and the API client behaviors users depend on — replacing "always authenticated" with real, revocable sessions.

## ADDED Requirements

### Requirement: Login screen
A `/login` route SHALL present a login screen following the design contract tokens, with email and password fields, a submit action that authenticates against the password provider, and a single generic error message for every failure ("Invalid email or password"), with loading and disabled states while submitting. Any authenticated page visited while logged out SHALL redirect to `/login`.

#### Scenario: Successful login
- **WHEN** valid credentials are submitted
- **THEN** the app lands on the first agent chat of the user's first membership

#### Scenario: Uniform error
- **WHEN** any login failure occurs (unknown email, wrong password, password-less account, disabled account)
- **THEN** one identical error message renders — failure modes are not distinguishable in the UI

#### Scenario: Auth gate
- **WHEN** any other route is opened while logged out
- **THEN** the app redirects to /login

### Requirement: Session boot
With a stored token, app boot SHALL validate it via `GET /auth/me` before rendering the app shell: on success the user and their memberships hydrate the session store; on 401 the token is discarded and the app shows /login. Boot SHALL NOT render app content from seed membership data before hydration completes.

#### Scenario: Reload with valid session
- **WHEN** the user reloads with a valid token and hydration resolves
- **THEN** no shell rendered before /auth/me resolved, and the app opens in the remembered workspace if the user is still a member, else the first membership

#### Scenario: Expired token
- **WHEN** the stored token is expired at boot
- **THEN** /login renders; the app does not flash seeded content

### Requirement: Logout
A logout control SHALL discard the stored token and return to /login. Server-side state is unaffected (stateless JWT).

#### Scenario: Logout
- **WHEN** the user logs out
- **THEN** the app returns to /login and back-navigation shows only /login

### Requirement: API error envelope
API failures SHALL surface through the existing toast system: invalid_request/conflict/forbidden/last_owner_protected map to specific human copy, and unknown codes fall back to the server message. Network failure SHALL produce its own toast. While a request is in flight the triggering control SHALL show a busy state.

#### Scenario: Conflict toast
- **WHEN** a request fails with code `last_owner_protected`
- **THEN** a toast explains the last owner cannot be removed or demoted

#### Scenario: Offline
- **WHEN** the API is unreachable
- **THEN** a network toast appears and the control un-busies
