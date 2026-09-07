# Design: enrich-agent-tools

## Context

The runner composes an agent per execution: `runner.resolve()` loads workspace/agent/user, resolves tools from the global `ToolRegistry`, and `Compose` wires middlewares (patchtoolcalls → reduction → summarization → skill → filesystem) around a jailed filesystem backend rooted at the agent workspace directory (`<onClawDir>/<tenant>/agents/<agentSlug>`). The ADK runner already persists interrupt checkpoints (`CheckPointStore: sessionAdapter`), but nothing emits interrupts and nothing resumes them.

## Decisions

### D1 — Allowlist semantics (chosen: pure allowlist, empty = none)

`agents.tools` is exactly the set of allowed registry tool names; empty means none. This matches the wizard's toggle model (checked = allowed, no NULL-vs-empty ambiguity) and makes shell triple-gated: not registered per agent unless `execute` is listed, approval-gated when dangerous, and jailed regardless.

- Reserved name `execute` in `tools` enables the filesystem middleware's shell tool: `buildMiddlewares` passes `Shell:` to `fsmw.MiddlewareConfig` only when the agent's allowlist contains `execute`. Filesystem/skill middleware tools are never allow-listed — they are jail-bound core capabilities.
- Unknown names in `tools` are inert (same rule the denylist had) — resolution happens against the runtime registry at execution time.
- Consequence accepted: existing agents migrate to `tools = '{}'` and lose registry tools (including `web.search`) until re-enabled. The denylist is dropped, not backfilled — `disabled_tools` restrictions do not survive.

### D2 — Run-scoped tool construction

```go
type ToolContext struct {
    WorkspaceSlug, AgentSlug string
    AgentDir                 string // agent workspace jail root
    SessionID                string
}
type ToolConstructor func(ToolContext) (tool.BaseTool, error)
```

`ResolvedTools` gains the context parameter; `runner.resolve()` already holds every value. Stateless tools (`web.search`, `web.fetch`) ignore it; browser tools use `AgentDir` + `SessionID`. Registration and the selection filter stay global; only construction becomes per-run.

### D3 — Shell rides the eino filesystem middleware

eino's `adk/middlewares/filesystem` registers its `execute` tool when `MiddlewareConfig.Shell` (a `filesystem.Shell`) is set; the tool schema, timeout plumbing, truncation, and background-run management are eino's. `ExecuteRequest` carries only `Command` + `Timeout` — no cwd — so confinement is entirely our implementation's job:

```go
// backend.JailedShell implements filesystem.Shell
cmd := exec.CommandContext(ctx, shell, "-c", command)
cmd.Dir    = agentDir            // working directory is the jail root
cmd.Env    = scrubbedEnv()       // minimal PATH/HOME/TMPDIR, no secrets
output cap + timeout enforced here; ExecuteResponse carries Output/ExitCode/Truncated/TimedOut
```

The jail is a working-directory convention, not an OS sandbox — commands run as the server process's user. Documented in the proposal; OS sandboxing is a non-goal.

### D4 — HITL: dangerous commands interrupt, checkpoint survives, resume executes

The classifier sits in front of `exec` inside `JailedShell.Execute`. A built-in pattern list flags dangerous commands (`rm -rf`, piping curl/wget into a shell, `sudo`, `mkfs`, `dd`, `chmod 777`, `shutdown`, fork bombs, …). Flagged commands return `compose.Interrupt(ctx, info)` instead of executing — the sentinel propagates through the middleware's bash tool to the ADK runner, which checkpoints via the already-wired store.

New plumbing:

- `TranscriptEventApprovalRequired` event kind carrying the interrupt ID and command; emitted from `streamRun` when `event.Action.Interrupted` is observed, persisted to session history, and it closes the current stream (the turn pauses; no terminal event of the existing kinds is emitted).
- `Runner.Resume(ctx, req, decision)` — mirrors `Run`'s load/resolve/compose (deterministic from stored config), then `runner.ResumeWithParams(checkpointID, &adk.ResumeParams{Targets: {interruptID: approved}})`.
- Approval resolution endpoint: `POST /workspaces/:ws/agents/:agent/sessions/:sid/approvals/:interruptID` with `{approved: bool}`, permission-gated like chat. Approval state is derived from history: the latest `approval_required` event without a subsequent resolution marker is pending; resolution persists the marker and resumes. Checkpoints are durable (Postgres), so approvals survive restarts.
- On resume: approved → the shell command executes and the tool result flows into the transcript as a normal tool-call card; denied → the tool result is a denial notice.

### D5 — Browser: go-rod, one session per execution, screenshots in the jail

go-rod is a CDP driver: `rod.New().ControlURL(cdpURL)` attaches to a remote endpoint; with no `ONCLAW_BROWSER_CDP_URL`, `launcher.New()` discovers (or downloads) a local Chromium. When neither is possible, tools return a clear "browser unavailable" error rather than failing the run.

- Four tools: `browser.navigate` (url → page title/status), `browser.act` (click/type/scroll via CSS selector or text ref), `browser.read` (readable text/structure of the current page), `browser.screenshot` (full-page PNG).
- Session manager keyed by `ToolContext.SessionID`: one rod browser + page per execution, user-data-dir under the OS temp dir, `Close()` deferred in `streamRun`'s teardown path. Sessions do not span executions.
- Screenshots write to `<AgentDir>/browser/screenshot-<seq>.png` — inside the jail, so file tools can read them and the result returns the in-jail path. Inline image rendering in transcript cards is a non-goal.

### D6 — web.search providers, web.fetch SSRF guard

`web.search` gains a `SearchProvider` seam: `tavily` (API key from `ONCLAW_TAVILY_API_KEY`) and `duckduckgo` (existing HTML scraping, zero-credential fallback), selected by `ONCLAW_SEARCH_PROVIDER`. The provider is injected at construction; per-call behavior is unchanged.

`web.fetch`: GET → readable-text extraction via `golang.org/x/net/html` (already a dependency), size-capped and truncated with a marker. Egress guard denies loopback, private, and link-local targets by default (`ONCLAW_FETCH_ALLOW_PRIVATE=true` opts out) — a server-side fetcher must not reach instance metadata or internal services. Redirects are re-validated per hop.

## Risks / Trade-offs

- Empty-means-none migration removes `web.search` from existing agents until re-enabled — accepted with the operator-facing note in the proposal.
- `compose.Interrupt` propagation through the middleware's bash tool wrapper is the designed path but not yet exercised in this codebase — first task of the HITL group is a spike proving the sentinel surfaces as `event.Action.Interrupted`.
- go-rod's browser download on first launch is an operator surprise; documenting `ONCLAW_BROWSER_CDP_URL` as the production path and the discovery order (CHROME_PATH → PATH lookup → managed download) in the README covers it.
- DuckDuckGo scraping stays fragile; Tavily is the reliability path, DDG remains the zero-config default.
