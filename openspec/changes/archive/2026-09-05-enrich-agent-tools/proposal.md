# Proposal: enrich-agent-tools

## Why

The agent tool surface is one tool (`web.search`, DuckDuckGo-only). Agents cannot run commands, fetch pages, or drive a browser, so whole classes of work (repo maintenance, research, web verification) are out of reach. The current spec additionally bans shell outright, and tool selection is denylist-only (`disabled_tools`), which does not match the product's per-agent capability model: an agent should carry the explicit list of tools it is allowed to use.

## What Changes

- **Tool allowlist replaces the denylist.** `agents.tools` (text[]) becomes the allowed registry tools for an agent; `disabled_tools` is dropped (column, domain field, API payloads). An empty `tools` means no registry tools — capabilities are deliberate. Unknown names are inert. This deliberately reverses the earlier decision that removed the `tools` allowlist field (recorded in the `agents` spec).
- **Tool constructors become run-scoped.** `ToolConstructor` receives a `ToolContext` (workspace slug, agent slug, agent dir, session id) resolved per execution, so tools can scope themselves to the agent workspace.
- **Shell, jailed and approval-gated.** The eino filesystem middleware's `execute` tool is enabled by passing a `filesystem.Shell` implementation whose commands run with the agent workspace as working directory. Dangerous commands (built-in pattern list) pause the execution for human approval via `compose.Interrupt` — the checkpoint store is already wired; new: an `approval_required` transcript event, a resume path on the runner, and an approval-resolution endpoint.
- **Web fetch tool.** `web.fetch` retrieves a URL and returns readable text, with an SSRF guard (loopback/private/link-local denied by default, instance opt-out).
- **Web search becomes multi-provider.** `web.search` gains a provider seam: `tavily` (API key) and `duckduckgo` (zero-credential fallback), selected by instance configuration.
- **Browser automation over CDP.** `browser.navigate`, `browser.act`, `browser.read`, `browser.screenshot` built on go-rod (CDP driver): attach to `ONCLAW_BROWSER_CDP_URL` when set, otherwise launch a discovered local Chromium. One browser session per execution, torn down when the execution ends. Screenshots are written into the agent workspace directory and the tool result returns the in-jail path.
- **UI.** The wizard's Step 3 tool toggles write the allowlist (checked = allowed). The transcript renders a pending-approval card for interrupted `execute` calls with approve/deny actions.

## Capabilities

### New Capabilities

- (none — all work lands in existing capabilities)

### Modified Capabilities

- `agent-runtime`: tool selection switches from denylist to allowlist; the shell ban is replaced by jailed shell execution with human approval; new built-in tools (web.fetch, web.search providers, browser CDP set) and the approval flow.
- `agents`: agent fields — `tools` allowlist replaces `disabled_tools`; create defaults and update patch semantics follow.
- `web-app/agents`: Step 3 tool toggles write the allowlist.
- `web-app/chat`: pending-approval card in the transcript.

## Impact

- **Breaking:** `disabled_tools` disappears from the schema, API, and spec. Existing agents migrate to an empty `tools` (no registry tools enabled); operators re-enable per agent via the wizard or API. Post-migration no agent has shell until `execute` is allow-listed on it — allowlist and approval form two independent gates for shell.
- **Scope of allowlist:** `tools` governs registry built-ins plus the reserved name `execute` (shell). Filesystem and skill middleware tools are not allow-listed — they are jail-bound core capabilities.
- **New dependency:** `github.com/go-rod/rod` (CDP driver). Existing: `golang.org/x/net/html` reused for fetch/readable-text.
- **New env:** `ONCLAW_SEARCH_PROVIDER` (`duckduckgo` default | `tavily`), `ONCLAW_TAVILY_API_KEY`, `ONCLAW_BROWSER_CDP_URL` (optional), `ONCLAW_FETCH_ALLOW_PRIVATE` (default false).
- **Trust statement:** shell commands run with the server process's OS user; the workspace jail is a working-directory convention, not an OS sandbox. OS-level sandboxing remains a non-goal for this change.
- Migration: one up/down pair (next number after 000016) adding `tools` and dropping `disabled_tools`.
