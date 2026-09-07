# agent-runtime Delta

## REMOVED Requirements

### Requirement: Tool denylist
The runtime SHALL expose every registered built-in tool to every agent by default. Tools named in the agent's `disabled_tools` SHALL NOT be exposed. Names in `disabled_tools` that match no registered tool SHALL be ignored, not errors.

**Reason:** replaced by explicit per-agent tool selection — the `tools` allowlist (see ADDED "Tool selection"). The denylist model is removed together with the `disabled_tools` column and API field.

## MODIFIED Requirements

### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep) SHALL operate only on paths inside the agent's workspace directory; any resolved path escaping it SHALL be rejected as a tool error, not a crash. The agent's generated prompt documents, its agent-tier skills directory, and the summarization offload file all live inside this directory and are reachable through the file tools. (Shell execution is no longer banned outright — it is governed by the "Shell execution" and "Dangerous-command approval" requirements below.)

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

## ADDED Requirements

### Requirement: Tool selection
An agent's tool surface SHALL be resolved from its `tools` allowlist. A name listed in `tools` SHALL expose the corresponding registered built-in tool; the reserved name `execute` SHALL enable the shell tool (see "Shell execution"). An agent whose `tools` array is empty SHALL have no registry tools exposed. Names in `tools` that match no registered tool SHALL be ignored, not errors. Built-in tools registered after an agent's allowlist was saved SHALL NOT appear for that agent until its allowlist is updated.

#### Scenario: Allowlist gates the surface
- **WHEN** an agent's `tools` contains only `web.search`
- **THEN** its executions expose `web.search` and no other registry tool

#### Scenario: Empty allowlist exposes nothing
- **WHEN** an agent has an empty `tools` array
- **THEN** no registry tool is available to its executions (filesystem and skill middleware tools remain, as jail-bound core capabilities)

#### Scenario: Unknown names are inert
- **WHEN** an agent is saved with `tools` naming a tool no registry provides
- **THEN** the save succeeds; the unknown name is inert at execution time

#### Scenario: Later-registered tools do not leak
- **WHEN** a new built-in tool is registered after an agent's allowlist was saved
- **THEN** the agent's executions do not expose it until the allowlist names it

### Requirement: Shell execution
When the agent's `tools` allowlist contains the reserved name `execute`, the runtime SHALL expose a shell tool. Shell commands SHALL execute with the agent's workspace directory as the working directory, SHALL inherit a scrubbed minimal environment (no instance secrets), and SHALL be bounded by a timeout and an output cap, with truncation and exit code reported in the tool result. Agents whose allowlist omits `execute` SHALL have no shell tool. The workspace jail SHALL be understood as a working-directory convention, not an OS sandbox.

#### Scenario: Allow-listed shell runs in the jail
- **WHEN** an agent with `execute` in `tools` runs a shell command that writes a file
- **THEN** the command executes with the agent's workspace directory as its working directory and the file appears inside the jail

#### Scenario: Shell not allow-listed
- **WHEN** an execution's agent does not list `execute` in `tools`
- **THEN** no shell tool is present on its tool surface

#### Scenario: Timeout bounds the command
- **WHEN** a shell command runs longer than the configured timeout
- **THEN** the command is stopped and the tool result reports the timeout with whatever output was produced

### Requirement: Dangerous-command approval
The shell tool SHALL classify each command against a built-in dangerous-pattern list (destructive filesystem operations, piping network fetches into a shell, privilege escalation, disk/format utilities, host power/reboot, and similar). A matching command SHALL NOT execute immediately: the runtime SHALL pause the execution, persist an `approval_required` transcript event carrying the command and an interrupt identifier, and wait for a human decision. A pending approval SHALL remain resolvable across server restarts. An approve decision SHALL execute the command and record its output as the tool result; a deny decision SHALL record a denial notice as the tool result without executing. Resolution SHALL be possible through an authenticated endpoint permitted to workspace members.

#### Scenario: Dangerous command pauses for approval
- **WHEN** an allow-listed shell tool is asked to run a command matching the dangerous list
- **THEN** the command does not execute, an `approval_required` event is recorded in the transcript, and the turn pauses

#### Scenario: Approval executes the command
- **WHEN** a member approves a pending approval
- **THEN** the command executes and its output appears in the transcript as the tool result

#### Scenario: Denial records a refusal
- **WHEN** a member denies a pending approval
- **THEN** the command never runs and the transcript records a denial notice as the tool result

#### Scenario: Approval survives a restart
- **WHEN** the server restarts while an approval is pending
- **THEN** the approval can still be resolved afterwards and the execution resumes from its checkpoint

### Requirement: Web fetch tool
The runtime SHALL expose a `web.fetch` tool that retrieves an http(s) URL and returns its readable text content, size-capped with an explicit truncation marker. The fetcher SHALL deny loopback, private, and link-local addresses by default — re-validating at each redirect hop — with an instance configuration flag to opt out for self-hosted internal use.

#### Scenario: Public page returns readable text
- **WHEN** the tool fetches a public HTML page
- **THEN** the result carries the page's readable text, truncated with a marker when over the size cap

#### Scenario: Internal address denied by default
- **WHEN** the tool is asked to fetch a loopback, private, or link-local URL
- **THEN** the fetch is refused with an error naming the egress guard

#### Scenario: Redirect re-validation
- **WHEN** a public URL redirects to a private address
- **THEN** the redirected fetch is refused under the same guard

### Requirement: Web search tool providers
The `web.search` tool SHALL resolve queries through a configurable search provider: `tavily` (API key) or `duckduckgo` (zero-credential default). Provider selection SHALL be instance configuration made at tool construction; the result shape SHALL be identical across providers.

#### Scenario: Tavily provider configured
- **WHEN** the instance selects `tavily` with a valid API key
- **THEN** `web.search` results come from the Tavily API in the standard result shape

#### Scenario: Default falls back to DuckDuckGo
- **WHEN** no provider is configured
- **THEN** `web.search` uses the zero-credential DuckDuckGo backend

#### Scenario: Misconfigured provider fails construction
- **WHEN** `tavily` is selected without an API key
- **THEN** tool construction fails with an error naming the missing configuration

### Requirement: Browser automation
The runtime SHALL expose browser tools — `browser.navigate`, `browser.act`, `browser.read`, and `browser.screenshot` — driving a Chrome DevTools Protocol browser. The runtime SHALL attach to a configured CDP endpoint when one is provided and otherwise launch a discovered local Chromium; when no browser is available, the tools SHALL return a clear availability error instead of failing the run. Each execution SHALL get its own isolated browser session, torn down when the execution ends. Screenshots SHALL be written into the agent's workspace directory, and the tool result SHALL return the in-jail path.

#### Scenario: Navigate and read a page
- **WHEN** the tools navigate to a URL and then read the page
- **THEN** `browser.navigate` reports the destination (title/status) and `browser.read` returns the page's readable content

#### Scenario: Session is per-execution
- **WHEN** an execution ends
- **THEN** its browser session and any launched browser process are stopped, and a later execution starts a fresh session

#### Scenario: Screenshot lands in the workspace
- **WHEN** `browser.screenshot` captures the current page
- **THEN** the PNG is written inside the agent's workspace directory and the result returns that path

#### Scenario: No browser available
- **WHEN** no CDP endpoint is configured and no local Chromium can be found
- **THEN** browser tool calls return an availability error describing the configuration needed
