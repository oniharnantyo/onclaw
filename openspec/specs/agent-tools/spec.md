# agent-tools Specification

## Purpose

Give agents real tool execution: turns invoke the built-in tool catalog — jailed workspace filesystem, shell, todos, and web access — and feed results back to the model until the turn concludes.

## Requirements

### Requirement: Tool execution loop

A turn SHALL resolve the agent's tool surface, offer it to the model, execute the tool calls the model makes, feed each result back, and continue until the model answers without tool calls or an iteration cap is reached. Each tool call and its result SHALL be recorded on the session transcript as it happens, with the result content persisted. When the iteration cap is reached, the turn SHALL end with a visible notice instead of failing silently.

#### Scenario: Tool call round trip

- **WHEN** the model calls a tool mid-turn
- **THEN** the tool executes, the result is fed back to the model, and the turn continues toward a final answer

#### Scenario: Runaway loop is bounded

- **WHEN** a turn's tool-call iterations exceed the cap
- **THEN** the turn ends with a visible cap notice rather than continuing indefinitely

#### Scenario: Tool failure feeds back

- **WHEN** a tool invocation fails
- **THEN** the error is returned to the model as the tool result and the turn continues

### Requirement: Tool gate

A built-in tool SHALL be offered to a turn only when it is in the catalog, enabled globally in the Tools pane, and permitted by the agent's tool allowlist (an empty allowlist permits all catalog tools). MCP tools are gated separately by their own capability.

#### Scenario: Globally disabled tool is not offered

- **WHEN** a tool is disabled in the Tools pane
- **THEN** subsequent turns of every agent are offered no such tool

#### Scenario: Allowlist narrows the surface

- **WHEN** an agent's tool allowlist lists a subset of tools
- **THEN** only that subset is offered on its turns

### Requirement: Workspace jail

The filesystem tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `delete_file`) SHALL operate only inside the agent's workspace directory, presented to the model as its workspace root, and inside the read-only knowledge corpus mount presented to the model as `/knowledge` (composed of the global documents and the session agent's scoped documents). Write operations (`write_file`, `edit_file`, `delete_file`) SHALL apply to the workspace only — the knowledge mount SHALL never be writable by filesystem tools. Paths resolving outside the jail SHALL return an error result, not end the turn.

#### Scenario: File edits stay in the workspace

- **WHEN** the model writes and then reads a file in the workspace
- **THEN** the operations succeed against the agent's workspace directory on disk

#### Scenario: Escape attempt is rejected

- **WHEN** the model requests a path outside the workspace
- **THEN** the tool returns an out-of-scope error result and the turn continues

#### Scenario: Knowledge corpus is readable through the tools

- **WHEN** the model lists, greps, or reads a path under `/knowledge`
- **THEN** the operation resolves against the global and session-agent knowledge roots on disk

#### Scenario: Knowledge mount is not writable

- **WHEN** the model attempts `write_file`, `edit_file`, or `delete_file` under `/knowledge`
- **THEN** the tool returns an error result, the corpus is unchanged, and the turn continues

### Requirement: Jailed shell

The `execute` tool SHALL run shell commands with the workspace as the working directory, a bounded timeout, and a bounded output size; the child environment SHALL NOT leak the host app's credentials. Timeouts and output truncation SHALL be reported in the tool result.

#### Scenario: Command output is captured

- **WHEN** the model runs a shell command
- **THEN** the combined output within the size cap is returned as the tool result

#### Scenario: Hanging command is killed

- **WHEN** a command exceeds the timeout
- **THEN** the process is killed and the tool result reports the timeout

### Requirement: Task plan tools

The `task_create`, `task_get`, `task_update`, and `task_list` tools SHALL let the agent maintain a structured task plan that persists in the agent's workspace across turns. The four tools SHALL be gated through the turn's tool gate exactly like every other built-in tool: each is offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist, and the Tools pane card list and the agent create/edit allowlist chips SHALL list them in place of the retired todo pair. Plans written in the retired todo format are not migrated; the plan starts empty.

#### Scenario: Plan persists across turns

- **WHEN** the agent creates tasks in one turn and lists them in a later turn
- **THEN** the same tasks are returned

#### Scenario: Task lifecycle transitions

- **WHEN** the agent creates a task, moves it in progress, and completes it
- **THEN** task_list reflects each transition

#### Scenario: Gated through the tool gate

- **WHEN** the task tools are disabled in the Tools pane, or an agent's allowlist omits them
- **THEN** that agent's turns are offered none of the four tools

### Requirement: Web fetch

The `web_fetch` tool SHALL retrieve a URL and return its content as readable text. Fetch failures SHALL be returned as tool results.

#### Scenario: Page is fetched

- **WHEN** the model fetches a reachable URL
- **THEN** the tool result contains the page's textual content

#### Scenario: Unreachable URL

- **WHEN** the model fetches a URL that cannot be reached
- **THEN** the tool result reports the failure and the turn continues

### Requirement: Web search

The `web_search` tool SHALL resolve queries through a configurable provider chain — brave, exa, perplexity, and firecrawl by API key, searxng by base URL — trying providers in order and failing over on error. With no provider configured, or when every provider in the chain fails, the tool SHALL return an explanatory error result.

#### Scenario: Search returns results

- **WHEN** the model searches with a configured provider
- **THEN** the tool result lists organic results (title, URL, snippet)

#### Scenario: Failover to the next provider

- **WHEN** the first provider in the chain fails
- **THEN** the next configured provider is tried and its results are returned

#### Scenario: No provider configured

- **WHEN** web_search runs with no provider configured
- **THEN** the tool result explains that a search provider must be configured in Tools settings

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

### Requirement: Scheduler tool in the catalog

The built-in tool catalog SHALL include a `scheduler` tool (display name "Scheduler") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The agent create/edit allowlist chips and the Tools pane card list SHALL list it without bespoke wiring.

#### Scenario: Scheduler appears in the catalog surfaces

- **WHEN** the Tools pane or the agent tool allowlist is rendered
- **THEN** a "Scheduler" entry is listed alongside the other built-in tools

#### Scenario: Disabled or unallowlisted scheduler is not offered

- **WHEN** the scheduler tool is disabled in the Tools pane, or an agent's allowlist omits it
- **THEN** that agent's turns are offered no `scheduler` tool

### Requirement: Scheduler actions

The `scheduler` tool SHALL expose a single `action` argument with the values `create`, `list`, `update`, and `delete`, taking only scalar arguments: `id`, `name`, `agent` (an agent name or id), `prompt`, `cron` (a five-field expression in local time), and `enabled`. `create` SHALL apply the same validation as the Schedules editor, SHALL default `agent` to the agent making the call, SHALL default `enabled` to true, and SHALL return the created schedule's id, human-readable recurrence, and next fire time. `list` SHALL return every schedule with id, name, agent, expression, human-readable recurrence, enabled flag, next fire time, and last run. `update` SHALL change only the supplied fields of the schedule identified by `id`. `delete` SHALL remove the schedule identified by `id`. Validation failures and unknown ids SHALL be returned as readable tool errors rather than failing the turn, and every change made through the tool SHALL be visible in the Schedules menu immediately.

#### Scenario: Agent creates a schedule for itself from chat

- **WHEN** the user asks the agent to check a URL every weekday morning and the model calls `scheduler` with `action: create`, a `name`, a `prompt`, and `cron: 0 9 * * 1-5`, omitting `agent`
- **THEN** a schedule for the calling agent is created and enabled, the result reports its id, the recurrence described as weekdays at 09:00, and the next fire time, and the schedule is listed in the Schedules menu

#### Scenario: Invalid expression is a readable error

- **WHEN** the model calls `action: create` with `cron: every monday`
- **THEN** the tool returns an error result explaining the expression is invalid and showing the expected five-field form, nothing is created, and the turn continues

#### Scenario: Duplicate name points at update

- **WHEN** the model calls `action: create` with a name that an existing schedule already uses
- **THEN** the tool returns an error result naming the existing schedule's id and suggesting `update`

#### Scenario: List reports schedule state

- **WHEN** the model calls `action: list`
- **THEN** the result carries every schedule with its id, name, agent, expression, human-readable recurrence, enabled flag, next fire time, and last run outcome

#### Scenario: Update disables by id

- **WHEN** the model calls `action: update` with an existing `id` and `enabled: false`
- **THEN** the schedule is disabled, its other fields are unchanged, and the Schedules menu shows it off

#### Scenario: Delete of an unknown id is a readable error

- **WHEN** the model calls `action: delete` with an id that does not exist
- **THEN** the tool returns an error result saying so and the turn continues

### Requirement: Multimodal file reads

The `read_file` tool SHALL render supported binary files as structured multimodal parts instead of raw bytes: image files as image parts, and PDF files as rendered page parts. A PDF read SHALL support reading the whole document or a bounded page range, with the per-request page bound reported in the tool result when the range is clipped. The jail applies unchanged: only files inside the agent's workspace or the read-only `/knowledge` mount are readable, and files exceeding the configured size limits SHALL return a clear error result rather than ending the turn. Text files SHALL continue to read as text.

#### Scenario: PDF pages render as parts

- **WHEN** the model reads a PDF in the workspace with a page range
- **THEN** the result carries rendered parts for the requested pages only

#### Scenario: Image reads as a part

- **WHEN** the model reads an image file in the workspace
- **THEN** the result carries the image as a structured part rather than bytes

#### Scenario: Oversize file is rejected

- **WHEN** the model reads a file larger than the configured size limit
- **THEN** the tool returns a clear error result and the turn continues

#### Scenario: Corpus PDF renders as parts

- **WHEN** the model reads a PDF under `/knowledge` with a page range
- **THEN** the result carries rendered parts for the requested pages, read from the knowledge roots

### Requirement: Save document tool in the catalog

The built-in tool catalog SHALL include a `save_document` tool (display name "Save document") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The tool SHALL accept either a chat attachment identified by its capability key or inline text content with a filename, MAY accept a short description recorded with the document, and SHALL save into the session agent's scope. It SHALL enforce the knowledge corpus format and size gates (text and PDF only); unsupported or oversize input returns a readable error result and the turn continues. A successful save registers the document in the catalog and returns its path under `/knowledge`. The tool is the only write path into the corpus: filesystem tools cannot create or modify knowledge documents.

#### Scenario: Agent saves a chat attachment

- **WHEN** the model calls `save_document` with the capability key of a PDF attached to the conversation
- **THEN** the document is registered in the agent's scope and the tool result reports its `/knowledge` path

#### Scenario: Agent saves composed text

- **WHEN** the model calls `save_document` with inline text content and a filename
- **THEN** the document is stored and registered like an uploaded text document

#### Scenario: Unsupported attachment is refused

- **WHEN** the model calls `save_document` with a video attachment's key
- **THEN** the tool returns a readable error naming the supported kinds and no document is created

### Requirement: Create document tool in the catalog

The built-in tool catalog SHALL include a `create_document` tool (display name "Create Document") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The tool is workspace-scoped: an agent without a rootable workspace SHALL have it withheld. The agent create/edit allowlist chips and the Tools pane card list SHALL list it without bespoke wiring.

#### Scenario: Create document appears in the catalog surfaces

- **WHEN** the Tools pane or the agent tool allowlist is rendered
- **THEN** a "Create Document" entry is listed alongside the other built-in tools, with a configuration action on its Tools pane card

#### Scenario: Disabled or unallowlisted create document is not offered

- **WHEN** the tool is disabled in the Tools pane, or an agent's allowlist omits it
- **THEN** that agent's turns are offered no `create_document` tool

#### Scenario: Slugless agent has the tool withheld

- **WHEN** an agent without a rootable workspace receives a turn
- **THEN** the `create_document` tool is withheld and the turn does not fail

### Requirement: Create document tool

The `create_document` tool SHALL render markdown into a PDF document using an embedded rendering library (no external binary) and write the result into the agent's workspace. Input SHALL be exactly the markdown content, a target file name, and an optional preset name. The file name SHALL be a plain name written at the workspace root; directory components, path traversal, and names resolving outside the workspace SHALL return an error result with no file written. The result SHALL carry the written file's workspace-relative name and size. A named preset that does not exist SHALL return a readable error with no file written. A call without a preset name SHALL use the configured default preset, falling back to the rendering library's built-in defaults when none is configured. Rendering failures, including a conversion exceeding its timeout, SHALL be returned as readable tool errors rather than failing the turn. Regenerating to an existing file name SHALL overwrite it.

#### Scenario: Markdown renders to a workspace PDF

- **WHEN** the model calls create_document with markdown content and file name "q3-report.pdf"
- **THEN** a PDF exists at that workspace-relative path and the tool result carries the name and size

#### Scenario: Unknown preset is a readable error

- **WHEN** the model names a preset that does not exist
- **THEN** the tool returns an error result naming the miss, nothing is written, and the turn continues

#### Scenario: File name cannot escape the workspace

- **WHEN** the model passes a file name carrying directory components or resolving outside the workspace
- **THEN** the tool returns an out-of-scope error result and no file is written

#### Scenario: Conversion failure is a readable error

- **WHEN** the rendering library fails or the conversion exceeds its timeout
- **THEN** the tool returns a readable error result and the turn continues

#### Scenario: Regenerating overwrites the earlier file

- **WHEN** the model calls create_document with a file name that already exists in the workspace
- **THEN** the new document replaces the old file and the result reports the fresh size
