# Spec Delta

## Purpose

Add a `browser` built-in tool: CDP-driven Chromium automation with named persistent profiles whose cookies and login state survive across turns and app restarts, explicit cookie management, a private-network guard, an idle-reaped lifecycle, and per-tool settings with profile management in the Tools pane.

## ADDED Requirements

### Requirement: Browser tool in the catalog

The built-in tool catalog SHALL include a `browser` tool (display name "Browser") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The agent create/edit allowlist chips and the Tools pane card list SHALL list it without bespoke wiring.

#### Scenario: Browser appears in the catalog surfaces

- **WHEN** the Tools pane or the agent tool allowlist is rendered
- **THEN** a "Browser" entry is listed alongside the other built-in tools

#### Scenario: Disabled or unallowlisted browser is not offered

- **WHEN** the browser tool is disabled in the Tools pane, or an agent's allowlist omits it
- **THEN** that agent's turns are offered no `browser` tool

### Requirement: Browser actions

The `browser` tool SHALL expose a single `action` argument with the values `navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, and `cookies`, taking only scalar arguments. `snapshot` SHALL return the current page URL, title, and a bounded list of interactive elements each carrying a stable `ref`, `role`, `name`, and current `value`. Element-targeted actions (`click`, `type`) SHALL address elements by a `ref` from the latest snapshot and, together with `press_key`, `scroll`, and `back`, SHALL return a fresh snapshot after acting. Action failures (unknown action, missing or stale ref, timeout, no page loaded) SHALL be returned as readable tool errors rather than failing the turn.

#### Scenario: Navigate loads a page

- **WHEN** the model calls `browser` with `action: navigate` and a public `https` URL
- **THEN** the page loads and the result carries the final URL and the page title

#### Scenario: Snapshot exposes referenceable elements

- **WHEN** the model calls `action: snapshot` on a loaded page containing a link and a text field
- **THEN** the result lists both as elements with distinct `ref` values, their roles, and accessible names

#### Scenario: Click by ref returns the new state

- **WHEN** the model calls `action: click` with a `ref` from the latest snapshot
- **THEN** the element is clicked and the result carries the resulting URL, title, and a fresh element list

#### Scenario: Stale ref is a readable error

- **WHEN** the model calls `action: click` with a `ref` that no longer resolves (for example after a navigation)
- **THEN** the tool returns an error result explaining the ref is stale and the turn continues

#### Scenario: Screenshot is saved to disk

- **WHEN** the model calls `action: screenshot`
- **THEN** a PNG is written under `~/.buddies/browser/screenshots/` and the result carries its path and pixel dimensions

### Requirement: Persistent browser profiles

The browser tool SHALL run each named profile as its own Chromium process whose user-data directory is `~/.buddies/browser/profiles/<name>/`, so cookies, local storage, and login state persist across tool calls, turns, sessions, and application restarts. A profile named `default` SHALL be created automatically on first use. Every action MAY carry a `profile` argument; when absent the configured default profile SHALL be used, falling back to `default`. Profile names SHALL match `^[a-z0-9][a-z0-9_-]{0,31}$`; other names SHALL be rejected before any filesystem access.

#### Scenario: Cookies persist across turns

- **WHEN** a turn sets a cookie in profile `work` (by site login or `cookies set`) and a later turn in a different session navigates to the same site with `profile: work`
- **THEN** the cookie is still present and sent with the request

#### Scenario: Login state survives an app restart

- **WHEN** a profile's browser has stored login cookies and the application is restarted
- **THEN** the next action on that profile launches Chromium on the same user-data directory and the site still recognizes the session

#### Scenario: Default profile is used when none is given

- **WHEN** an action omits `profile`
- **THEN** it runs in the configured default profile (or `default` when none is configured), creating the directory if needed

#### Scenario: Invalid profile name is rejected

- **WHEN** an action passes `profile: "../etc"` or `profile: "My Profile"`
- **THEN** the tool returns a validation error and touches no files

### Requirement: Cookie management

The `cookies` action SHALL support `cookie_op` values `list`, `set`, `delete`, and `clear` against the profile's browser: `list` returns the cookies visible to the current page (or all cookies when no page is loaded) with name, value, domain, path, expiry, and flags; `set` stores a cookie from `cookie_name` and `cookie_value` scoped by `cookie_domain`/`cookie_path` (or the current page URL when omitted); `delete` removes a named cookie in that scope; `clear` removes every cookie in the profile.

#### Scenario: Set then list

- **WHEN** the model calls `cookies` with `cookie_op: set`, `cookie_name: session`, `cookie_value: abc`, `cookie_domain: example.com` and then `cookie_op: list`
- **THEN** the list result contains the `session` cookie for `example.com` with value `abc`

#### Scenario: Clear empties the jar

- **WHEN** the model calls `cookies` with `cookie_op: clear` and then `cookie_op: list`
- **THEN** the list result contains no cookies

### Requirement: Private-network guard

`navigate` SHALL accept only `http` and `https` URLs and SHALL refuse targets that resolve to loopback, private, link-local, or unique-local addresses unless the tool's `allowPrivate` setting or the `BUDDIES_BROWSER_ALLOW_PRIVATE` environment variable permits them. After a page load, the final URL's host SHALL be re-checked under the same policy and the page reset to `about:blank` on violation.

#### Scenario: Private URL refused by default

- **WHEN** the model calls `navigate` with `http://127.0.0.1:9245/` and `allowPrivate` is off
- **THEN** the tool returns a refusal error and no navigation occurs

#### Scenario: Allow-private permits local targets

- **WHEN** `allowPrivate` is enabled in the browser settings and the model navigates to `http://localhost:9245/`
- **THEN** the navigation proceeds

#### Scenario: Non-HTTP scheme refused

- **WHEN** the model calls `navigate` with a `file://` URL
- **THEN** the tool returns a refusal error regardless of `allowPrivate`

### Requirement: Browser lifecycle

A profile's browser SHALL launch lazily on its first action (headless by default), be reused by subsequent actions on that profile, serialize concurrent actions on the same profile, and be closed after it has been idle longer than the configured idle timeout. Each action SHALL be bounded by the configured action timeout. Closing an idle browser SHALL not lose profile state.

#### Scenario: Idle browser is reaped and state survives

- **WHEN** no action touches profile `default` for longer than the idle timeout
- **THEN** its Chromium process exits, and the next action relaunches it on the same user-data directory with cookies intact

#### Scenario: Action timeout is enforced

- **WHEN** a `navigate` to a page that never finishes loading exceeds the action timeout
- **THEN** the tool returns a timeout error result and the browser remains usable for the next action

### Requirement: Browser settings and profile management

The Tools pane browser card SHALL offer a configuration action opening a Browser dialog with: default profile (chosen from existing profiles), headless toggle (default on), idle timeout seconds (default 120), action timeout seconds (default 30), allow-private toggle (default off), and a profile list with create and delete controls. Configuration SHALL persist under the `browser` key of the tool settings store and apply to the next launch of each profile. Creating a profile SHALL create its directory; deleting a profile SHALL close its running browser (if any) and remove its directory; the `default` profile SHALL not be deletable; deleting the configured default profile SHALL reset the default to `default`.

#### Scenario: Configuration round-trips

- **WHEN** the user sets headless off, idle timeout 300, and default profile `work` in the dialog and saves
- **THEN** reopening the dialog shows those values and the next browser launch of any profile honors them

#### Scenario: Profile create and delete

- **WHEN** the user creates profile `work` and later deletes it
- **THEN** `~/.buddies/browser/profiles/work/` exists after creation, and after deletion it is gone and no longer listed

#### Scenario: Default profile is protected

- **WHEN** the user attempts to delete the `default` profile
- **THEN** the dialog refuses and the profile remains
