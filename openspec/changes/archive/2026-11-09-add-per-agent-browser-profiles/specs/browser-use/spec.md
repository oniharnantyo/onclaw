# Spec Delta

## MODIFIED Requirements

### Requirement: Persistent browser profiles

Each named profile SHALL run as its own Chromium process whose user-data directory is `~/.buddies/browser/profiles/<name>/`, so cookies, local storage, and login state persist across tool calls, turns, sessions, and application restarts. A profile named `default` SHALL be created automatically on first use. Every tool MAY carry a `profile` argument; when absent the configured default profile SHALL be used, falling back to `default`. Profile names SHALL match `^[a-z0-9][a-z0-9_-]{0,31}$`; other names SHALL be rejected before any filesystem access.

A tool call whose `_meta` binds a profile directory (`buddies.dev/profile-dir`, host-stamped) SHALL ignore the `profile` argument and the profile-name rules entirely: the bound directory becomes that call's profile user-data directory — created on demand if missing — and the call's bound profile name (`buddies.dev/profile-name`, also host-stamped, display only) keys its live browser session. Bound and named profiles are isolated from each other: separate user-data directories never share cookies, storage, or a live browser. All persistence guarantees (cookies, local storage, login state across calls, turns, restarts), the cookie jar, and takeover relaunch behavior apply identically to bound profiles.

#### Scenario: Cookies persist across turns

- **WHEN** a turn sets a cookie in profile `work` and a later turn in a different session navigates to the same site with `profile: work`
- **THEN** the cookie is still present and sent with the request

#### Scenario: Login state survives an app restart

- **WHEN** a profile has stored login cookies and the application is restarted
- **THEN** the next action on that profile relaunches Chromium on the same user-data directory and the site still recognizes the session

#### Scenario: Default profile is used when none is given

- **WHEN** an action omits `profile`
- **THEN** it runs in the configured default profile (or `default` when none is configured), creating the directory if needed

#### Scenario: Bound call ignores the profile argument

- **WHEN** a call carries `_meta` `buddies.dev/profile-dir` and its arguments also carry `profile: other`
- **THEN** the action runs in the bound directory, `other` is not resolved, and no profile named `other` is created or touched

#### Scenario: Bound directory is created on demand

- **WHEN** the first browser action for an agent runs and the bound directory does not exist on disk
- **THEN** the server creates it (private to the user) and the action proceeds in a fresh profile

#### Scenario: Bound profiles are isolated

- **WHEN** two calls bind different profile directories (two agents) and both browse the same site
- **THEN** each runs in its own Chromium process and neither sees the other's cookies, storage, or pages

#### Scenario: Login state persists per agent

- **WHEN** a person signs in during a headed takeover of an agent's bound profile and the browser is later closed or reaped
- **THEN** the next action bound to that agent relaunches Chromium on the same directory and the site still recognizes the session

## ADDED Requirements

### Requirement: Agent profile binding

The parent SHALL bind every browser tool call it forwards on behalf of an agent to that agent's own profile: the per-call `_meta` SHALL carry the agent's workspace profile directory (`~/.buddies/workspaces/<agent slug>/browser-profile/`) and a display name derived from the agent. Every agent SHALL have its own profile; no two agents share one. The model SHALL NOT be able to read or influence the binding: the values are parent-stamped, the `profile` argument is not surfaced as a choice on bound calls, and takeover, secret, and cookie operations on behalf of an agent SHALL operate on the agent's bound profile. A call without the binding (direct CLI use, tests) SHALL keep the named-profile behavior unchanged.

#### Scenario: Agent actions use the agent's own profile

- **WHEN** an agent calls any browser tool during its turn
- **THEN** the call resolves to the agent's workspace profile directory regardless of any `profile` argument the model supplied

#### Scenario: Agents do not share a live browser

- **WHEN** two different agents use browser tools concurrently
- **THEN** each operates its own Chromium process and neither interrupts the other's pages

#### Scenario: Takeover binds to the requesting agent

- **WHEN** an agent requests a takeover and the person approves it
- **THEN** the headed relaunch opens the requesting agent's bound profile directory, and the sign-in performed there persists for that agent only

#### Scenario: Unbound calls keep named-profile behavior

- **WHEN** `browser-mcp` is invoked directly (CLI or test) with no binding metadata
- **THEN** profiles resolve by name under `~/.buddies/browser/profiles/` exactly as before this change
