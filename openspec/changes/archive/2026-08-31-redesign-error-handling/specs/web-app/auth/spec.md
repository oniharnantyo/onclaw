## MODIFIED Requirements

### Requirement: Session boot
With a stored token, app boot SHALL validate it via `GET /auth/me` before rendering the app shell: on success the user and their memberships hydrate the session store; on 401 the token is discarded and the app shows /login; on any other failure (network error, 5xx) the token is preserved and the app shows a boot error page — never the app shell and never /login. The boot error page SHALL render with the shared error-state grammar (illustration, detail chip) like every other state page. Boot SHALL NOT render app content from seed membership data before hydration completes, and the app shell SHALL render only with a verified user identity.

#### Scenario: Reload with valid session
- **WHEN** the user reloads with a valid token and hydration resolves
- **THEN** no shell rendered before /auth/me resolved, and the app opens in the remembered workspace if the user is still a member, else the first membership

#### Scenario: Expired token
- **WHEN** the stored token is expired at boot
- **THEN** /login renders; the app does not flash seeded content

#### Scenario: Server unreachable at boot
- **WHEN** `/auth/me` fails with a network error or 5xx at boot
- **THEN** a boot error page renders with the shared error-state grammar — not the app shell, never /login — and the stored token is preserved

#### Scenario: Boot error retry
- **WHEN** the user activates Retry on the boot error page and the server is reachable again
- **THEN** boot re-runs through the same boot routine; while retrying, the loading state renders, and the app opens normally without a page reload or re-login

#### Scenario: No anonymous identity
- **WHEN** the app shell is rendered
- **THEN** the identity control displays the verified user's name — no fallback identity ("You") appears when no user is verified

### Requirement: API error envelope
API failures SHALL surface through the toast system: invalid_request/conflict/forbidden/last_owner_protected map to specific human copy, and unknown codes fall back to the server message. While a request is in flight the triggering control SHALL show a busy state. When the server is unreachable mid-session (network failures while the app is open), failures SHALL render a single sticky connection banner instead of one toast per failed call; boot-time unreachability keeps the boot error page.

#### Scenario: Conflict toast
- **WHEN** a request fails with code `last_owner_protected`
- **THEN** a toast explains the last owner cannot be removed or demoted

#### Scenario: Offline
- **WHEN** the API is unreachable mid-session
- **THEN** a single sticky connection banner renders; failed calls do not stack network toasts, and the triggering controls un-busy
