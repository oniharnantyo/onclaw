# browser-use Specification

## Purpose

Browser Use is buddies' system MCP server for real-browser automation: a standalone server process buddies ships, spawns, and consumes through the standard MCP surface. It owns persistent Chromium profiles (each profile its own cookie jar), exposes discrete browser tools to agents, and imports existing Chrome profiles so users bring their own logins. Replaces the former built-in `browser` tool; see the agent-tools spec for the removal.

## Requirements

### Requirement: System server process

The app SHALL run Browser Use as its own MCP server process, spawned from the app's own binary and communicating over that child process's standard input and output. The server process SHALL be the sole owner of live browser state, and the app process itself SHALL NOT run a browser engine. The server SHALL expose no network listener; its only client is the app through the spawn pipe. The server process SHALL exit when its input stream closes, so app death leaves no orphan.

#### Scenario: Spawned at startup and probed

- **WHEN** the app starts
- **THEN** the browser-use server process is spawned, the MCP probe connects, and the pane reports connected with the discovered tool count

#### Scenario: Child crash is contained

- **WHEN** the server process dies while an action is in flight
- **THEN** the calling turn receives an error result, the app keeps running, and the server is respawned

#### Scenario: App exit leaves no orphan

- **WHEN** the app closes
- **THEN** the server process has exited and no browser-use process remains

### Requirement: Supervision

A dead server SHALL be respawned with increasing backoff; a bounded consecutive-failure count SHALL stop respawning and surface a failed connection state carrying the failure reason, and a successful probe SHALL reset the count.

#### Scenario: Crash then recovery

- **WHEN** the server process crashes once and a respawn succeeds
- **THEN** the row returns to connected and subsequent actions work without app restart

#### Scenario: Crash loop surfaces as failed

- **WHEN** the server process exits with failure more times than the cap allows
- **THEN** respawning stops and the connection state shows failed with the reason until a successful probe

### Requirement: Tool surface and errors

The server SHALL expose discrete tools `navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, `cookies`, `read`, and `wait`, each with its own parameter schema and description. `snapshot` SHALL return the page URL, title, and a bounded list of interactive elements each carrying a `ref`, `role`, `name`, and current `value`, with long names and values truncated. `read` SHALL return the page's text content bounded to a maximum length, truncated mid-page rather than failing. `wait` SHALL pause until network activity settles or a given delay elapses, whichever the arguments request. `screenshot` SHALL write a PNG into the calling session's workspace screenshots directory when the call's `_meta` names one, otherwise under the browser screenshots directory, and return its path and pixel dimensions. Action failures — missing arguments, stale targets, timeouts, no page loaded — SHALL return readable tool errors and MUST NOT fail the turn.

#### Scenario: Snapshot exposes referenceable elements

- **WHEN** `snapshot` runs on a loaded page containing a link and a text field
- **THEN** the result lists both as elements with distinct `ref` values, their roles, and accessible names

#### Scenario: Read returns bounded text

- **WHEN** `read` runs on a long article page
- **THEN** the result carries the page text up to the length bound, with a truncation indication

#### Scenario: Wait settles network activity

- **WHEN** `wait` is called while background requests are in flight
- **THEN** the tool returns after the network quiets (or the requested delay) and reports the page URL

#### Scenario: Screenshot saved to disk

- **WHEN** `screenshot` runs and the call's `_meta` names the calling session's workspace screenshots directory (`buddies.dev/screenshot-dir`, stamped by the parent; the model neither sees nor influences the value)
- **THEN** a PNG is written into that directory — `sessions/<session id>/browser/` under the calling agent's workspace, inside the read_file jail — and the result carries its path and pixel dimensions

#### Scenario: Screenshot without a session directory falls back

- **WHEN** `screenshot` runs with no session directory in the call's `_meta` (direct child use, the CLI, a test), or the supplied value is not a clean absolute path
- **THEN** a PNG is written under the browser dir's `screenshots/` and the result carries its path and pixel dimensions

### Requirement: Element targeting

`click` and `type` SHALL address elements either by a `ref` from the latest snapshot or by name/role targeting: a case-insensitive exact accessible-name match, falling back to a role-and-substring match, resolved deterministically against the latest snapshot. An ambiguous target SHALL be rejected with an error listing the candidate elements. Refs SHALL reset on navigation, and a target that no longer resolves SHALL be a readable stale-target error.

#### Scenario: Click by name in one round trip

- **WHEN** the model calls `click` with name "Sign in" on a page with one control of that name
- **THEN** the element is clicked, no prior snapshot call was required, and the result carries the outcome

#### Scenario: Ambiguous name lists candidates

- **WHEN** the model calls `click` with a name matching several elements
- **THEN** the tool returns an error enumerating the matching candidates and the turn continues

#### Scenario: Stale target is a readable error

- **WHEN** the model calls `click` with a `ref` that no longer resolves after a navigation
- **THEN** the tool returns an error explaining the target is stale and the turn continues

### Requirement: Action results and settling

Interactive actions (`click`, `type`, `press_key`, `scroll`, `back`) SHALL return a slim result — confirmation, final URL and title, and only the elements that changed — rather than a full element list; full lists ride only `snapshot` and `read`. After a navigation the page SHALL be given the chance to settle by waiting for network quiet, falling back to DOM stability on pages that never fire a load event, before the result is produced.

#### Scenario: Click result is slim

- **WHEN** the model calls `type` into a search field on a page with many interactive elements
- **THEN** the result carries ok, URL, title, and the changed elements, and not the full page element list

#### Scenario: SPA navigation settles

- **WHEN** a click triggers a client-side route change on a page that never fires load
- **THEN** the result reflects the settled new content rather than the transient intermediate state

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

### Requirement: Cookie management

The `cookies` tool SHALL support `op` values `list`, `set`, `delete`, and `clear` against the profile's browser: `list` returns the cookies visible to the current page (or all cookies when no page is loaded) with name, value, domain, path, expiry, and flags; `set` stores a cookie scoped by `domain`/`path` (or the current page URL when omitted) and made persistent by default; `delete` removes a named cookie in that scope; `clear` removes every cookie in the profile.

#### Scenario: Set then list

- **WHEN** the model sets cookie `session=abc` scoped to `example.com` and then lists cookies
- **THEN** the list result contains the `session` cookie for `example.com` with value `abc`

#### Scenario: Clear empties the jar

- **WHEN** the model clears cookies and then lists them
- **THEN** the list result contains no cookies

### Requirement: Private-network guard

`navigate` SHALL accept only `http` and `https` URLs and SHALL refuse targets that resolve to loopback, private, link-local, or unique-local addresses unless the server's allow-private setting or the `BUDDIES_BROWSER_ALLOW_PRIVATE` environment variable of the server process permits them. After a page load, the final URL's host SHALL be re-checked under the same policy and the page reset to `about:blank` on violation.

#### Scenario: Private URL refused by default

- **WHEN** the model navigates to `http://127.0.0.1:9245/` and allow-private is off
- **THEN** the tool returns a refusal error and no navigation occurs

#### Scenario: Allow-private permits local targets

- **WHEN** allow-private is enabled in Browser Use settings and the model navigates to `http://localhost:9245/`
- **THEN** the navigation proceeds

#### Scenario: Non-HTTP scheme refused

- **WHEN** the model navigates to a `file://` URL
- **THEN** the tool returns a refusal error regardless of allow-private

### Requirement: Browser lifecycle

A profile's browser SHALL launch lazily on its first action (headless by default), be reused by subsequent actions on that profile, serialize concurrent actions on the same profile, and be closed after it has been idle longer than the configured idle timeout. Each action SHALL be bounded by the configured action timeout. Closing an idle browser SHALL not lose profile state. Before an action is served from a cached browser, the server SHALL verify that browser still answers, and a browser that has died underneath the server (a crash, a person closing the window, a takeover relaunch swap) SHALL be discarded and the profile relaunched from its persisted state rather than failing the action on stale handles — except while the session is held by a handover arc (a person takeover or an in-flight secret protection), whose state decides its own resolution. A protocol error from a dead browser that still reaches the tool result SHALL be rendered as readable text naming the recovery, never as a raw protocol struct.

#### Scenario: Idle browser is reaped and state survives

- **WHEN** no action touches profile `default` for longer than the idle timeout
- **THEN** its Chromium process exits, and the next action relaunches it on the same user-data directory with cookies intact

#### Scenario: Browser dying underneath the server relaunches on the next action

- **WHEN** the profile's Chromium process crashes or its window is closed while the server still caches the session, and a later action targets that profile
- **THEN** the server relaunches Chromium on the same user-data directory and the action succeeds, instead of returning a stale-session protocol error

#### Scenario: Held session is never relaunched underneath the arc

- **WHEN** a profile's cached browser has died but the session is held by a person takeover or an in-flight secret protection
- **THEN** the server does not discard or relaunch that session; its handover state decides resolution

#### Scenario: Action timeout is enforced

- **WHEN** a navigation to a page that never finishes loading exceeds the action timeout
- **THEN** the tool returns a timeout error result and the browser remains usable for the next action

### Requirement: Single-owner lock

The server SHALL hold an exclusive ownership lock on the browser directory while running. A second server instance targeting the same directory SHALL refuse to start with a clear reason, which SHALL surface as a failed connection state. A lock left by a process that no longer exists SHALL be broken and retaken.

#### Scenario: Double run is refused

- **WHEN** a second server instance starts against the same browser directory while the first is alive
- **THEN** it exits with an ownership error and the app surfaces the failure reason in the connection state

#### Scenario: Stale lock is retaken

- **WHEN** a lock file names a process that no longer exists
- **THEN** the server breaks the lock and starts normally

### Requirement: Configuration at spawn boundaries

Browser Use configuration (default profile, headless, idle timeout, action timeout, allow-private) SHALL be supplied to the server at spawn. Configuration changes SHALL persist immediately and take effect at the next spawn boundary: the supervisor restarts the server when no profile browser is live, and defers otherwise. The server MUST NOT be restarted mid-action to apply a configuration change.

#### Scenario: Change applies after idle

- **WHEN** the user changes the headless setting while no profile browser is live
- **THEN** the server respawns promptly and the next profile launch honors the new setting

#### Scenario: Live session is not interrupted

- **WHEN** the user changes a setting while a profile browser is mid-task
- **THEN** the running browser continues unaffected and the change applies once the browser goes idle

### Requirement: Profile import

Browser Use settings SHALL offer importing an existing Chrome or Chromium profile: the user selects an existing user-data directory (or a profile directory within one), and the app copies its contents into `profiles/<name>/` under a validated profile name, appending a disambiguating suffix on name collision. The source directory SHALL be left untouched. The import UI SHALL disclose that the source browser should be closed first, that long-lived profiles copy a large payload, and that cookie decryption is best-effort (it succeeds for same-machine copies and may not carry encrypted cookies across machines or newer Windows protections). An imported profile SHALL thereafter behave as a normal profile: listed, usable, and deletable.

#### Scenario: Import copies and lists

- **WHEN** the user imports a Chrome profile directory as `work`
- **THEN** `~/.buddies/browser/profiles/work/` contains the copied data, and `work` appears in the profile list and resolves as a profile

#### Scenario: Collision is suffixed

- **WHEN** the user imports a profile with a name that already exists
- **THEN** the import creates a suffixed profile name instead of overwriting the existing profile

#### Scenario: Source stays intact

- **WHEN** an import completes or fails partway
- **THEN** the source directory is unmodified either way

#### Scenario: Imported profile is deletable

- **WHEN** the user deletes an imported profile
- **THEN** its directory is removed and it no longer appears in the profile list

### Requirement: Secure secret submission

The system server SHALL expose `submit_secret` whose input schema carries field targets and submission controls only — `targets`, `method` (`password|otp|authenticator`), `digits`, `submission` (`click|enter|on_input`), optional `submitTarget`, `identifierTarget`, `loginId`, `username` — and SHALL reject any value-bearing field. An `identifierTarget` names the login form's identifier field (username or email): the handover card then also collects an identifier, which the server SHALL fill before the secret and fingerprint exactly like every other target. Before showing a request to the user the server SHALL validate: HTTPS-only origin, no framed targets, no request already pending on the session, isolation from connected same-site tabs, and it SHALL fingerprint the form and re-check the fingerprint immediately before entry. The user's value and typed identifier SHALL be delivered only over the parent-initiated internal channel, SHALL be typed via trusted CDP input events, and SHALL never appear in tool arguments, tool results, snapshots, screenshots, session events, the message transcript, or logs: the tool result SHALL be a fixed-shape receipt (`status`, optional `savedAsLogin`), error paths SHALL return fixed reason codes, and inspection tools on the session SHALL be refused from request until scrub-release. After submission the server SHALL clear filled fields and scan the document (title, URL, text, values, attributes, open shadow roots): a clean same-document page MAY stay open with `evaluate` and recording blocked until a real main-frame navigation (which clears history); a page where the value is findable SHALL be reloaded with GET. Navigation whose URL carries the secret SHALL be blocked. Secrets SHALL NOT be retried. When saving to the vault fails, sign-in SHALL still proceed.

#### Scenario: Model cannot supply or observe a value

- **WHEN** a turn issues `submit_secret` with a value field, or any tool attempts snapshot/screenshot/read/evaluate on a session between request and scrub-release
- **THEN** the value field is rejected by schema, and the inspection tools answer with the protection refusal

#### Scenario: Fill, scrub, and receipt

- **WHEN** the user submits a password on a same-document sign-in page and the scrub scan finds the value nowhere
- **THEN** the fields are cleared, the page stays open with evaluate/recording blocked until the next real navigation, and the tool result is `{"status":"completed"}` with no value-bearing content anywhere in the session's events or transcript

#### Scenario: Echo falls back to reload

- **WHEN** the scrub scan finds the typed value echoed in a shadow-root text node
- **THEN** the page is reloaded with GET and the value appears in no snapshot or log

### Requirement: User takeover

The system server SHALL expose `request_takeover` (reason `sign-in-blocked`) which parks the calling turn on an attention card with Approve and Cancel. Approving SHALL relaunch the profile's browser headed and focused, invalidate references and snapshots, and hold the session: every tool on it SHALL return a fixed refusal until the user presses Done (resolving the request `completed` with "take a fresh snapshot") or Cancel (`cancelled`). The held state SHALL end only at resolution or tab close, with no timeout. A takeover card SHALL be shown only after the handover has taken effect.

#### Scenario: Agent blocked while the user signs in

- **WHEN** the user approves a takeover and signs in by hand in the headed window
- **THEN** every agent tool on that session returns the fixed refusal until Done is pressed

#### Scenario: Resolution hands the page back

- **WHEN** the user presses Done after signing in
- **THEN** the held state ends, the parked tool returns `{"status":"completed","next":"take a fresh snapshot"}`, and the agent re-inspects the page before acting
