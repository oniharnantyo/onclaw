## MODIFIED Requirements

### Requirement: Session boot
With a stored token, app boot SHALL validate it via `GET /auth/me` before rendering the app shell: on success the user and their memberships hydrate the session store; on 401 the token is discarded and the app shows /login; on any other failure (network error, 5xx) the token is preserved and the app shows a boot error page — never the app shell and never /login. Boot SHALL NOT render app content from seed membership data before hydration completes, and the app shell SHALL render only with a verified user identity.

#### Scenario: Reload with valid session
- **WHEN** the user reloads with a valid token and hydration resolves
- **THEN** no shell rendered before /auth/me resolved, and the app opens in the remembered workspace if the user is still a member, else the first membership

#### Scenario: Expired token
- **WHEN** the stored token is expired at boot
- **THEN** /login renders; the app does not flash seeded content

#### Scenario: Server unreachable at boot
- **WHEN** `/auth/me` fails with a network error or 5xx at boot
- **THEN** a boot error page renders — not the app shell, never /login — and the stored token is preserved

#### Scenario: Boot error retry
- **WHEN** the user activates Retry on the boot error page and the server is reachable again
- **THEN** boot re-runs through the same boot routine; while retrying, the loading state renders, and the app opens normally without a page reload or re-login

#### Scenario: No anonymous identity
- **WHEN** the app shell is rendered
- **THEN** the identity control displays the verified user's name — no fallback identity ("You") appears when no user is verified
