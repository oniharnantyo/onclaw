# Tasks: enrich-agent-tools

## 1. Tool allowlist (replaces denylist)

- [x] 1.1 Migration 000017: `ALTER TABLE agents ADD COLUMN tools text[] NOT NULL DEFAULT '{}'` + `DROP COLUMN disabled_tools`; write up/down
- [x] 1.2 Domain: replace `DisabledTools` with `Tools []string` on `domain.Agent`; update store port/Postgres adapter and test fake
- [x] 1.3 API: `tools []string` on create payload, `tools *[]string` on update payload (patch semantics mirroring the old field); remove `disabled_tools` everywhere; update handler tests
- [x] 1.4 Registry: `ToolContext` struct; `ToolConstructor` takes `ToolContext`; `ResolvedTools(ctx, reg, allowed)` switches to allowlist semantics (empty = none, unknown names inert); selection filter replaces denylist filter; update registry tests
- [x] 1.5 Runner: pass `ToolContext` from `resolve()`; compose shell only when `execute` ∈ `agent.Tools` (D3 wiring lands in task 3 — here just gate the middleware config field)
- [x] 1.6 UI: wizard Step 3 tool toggles write `tools` (checked = allowed, list initialized from registry names); update agents API client types

## 2. Web fetch tool

- [x] 2.1 `tools/webfetch.go`: `web.fetch` — GET, readable-text extraction via `x/net/html`, JSON/content-type passthrough for non-HTML, size cap + truncation marker; `HTTPClient` seam like `web.search`
- [x] 2.2 SSRF guard: validate scheme (http/https) and resolved IPs per hop (deny loopback/private/link-local unless `ONCLAW_FETCH_ALLOW_PRIVATE`); unit tests for the guard
- [x] 2.3 Register in `NewDefaultToolRegistry`; tool tests (fake HTTP client: HTML page, JSON body, oversize body, blocked host)

## 3. Shell (jailed execute)

- [x] 3.1 Spike (`internal/agents/shell_interrupt_spike_test.go`): `tool.Interrupt` returned from a `filesystem.Shell` surfaces as `event.Action.Interrupted` through the filesystem middleware's bash tool, checkpoint persists, resume re-runs with the decision as resume data
- [x] 3.2 `backend.JailedShell`: implements `filesystem.Shell` — `cmd.Dir = agentDir`, scrubbed env, timeout enforcement, output cap with `Truncated`, exit code mapping
- [x] 3.3 Danger classifier: built-in pattern list (unit-tested: rm -rf, curl|sh, sudo, mkfs, dd, chmod 777, shutdown, fork bomb; safe commands pass)
- [x] 3.4 Wire `MiddlewareConfig.Shell` in `buildMiddlewares` when `execute` ∈ allowlist; composer test coverage via the approval-flow end-to-end tests (shell absent by default, present when allow-listed)
- [x] 3.5 JailedShell tests: cwd confinement, env scrubbing, timeout, truncation, classifier passthrough

## 4. HITL approval flow

- [x] 4.1 `TranscriptEventApprovalRequired` (`approval_required`) event kind carrying interrupt ID + command; persisted through the session adapter (ADK interrupt events, translated in History)
- [x] 4.2 `streamRun`: observe `event.Action.Interrupted`, emit the event, end the stream without a terminal event of the existing kinds; execution state reflects paused-turn semantics
- [x] 4.3 `Runner.Resume(ctx, req, approval, approved)`: load/resolve/compose as `Run`, then `ResumeWithParams` targeting the interrupt; approved executes, denied returns denial result; end-to-end tests with the in-memory fake (dangerous command → interrupt → approve → output; deny → denial notice). Decisions also travel a durable ledger (checkpoint KV store keyed by command hash) because eino regenerates interrupt IDs when reconstructing a checkpoint in a new process.
- [x] 4.4 Approval resolution endpoint `POST .../sessions/:sid/approvals/:interruptID` `{approved}` — permission-gated (agents.write), 404 for unknown/unowned agents via the shared resolver, 409 when no pending approval matches; pending state derived from history (latest interrupt event without subsequent turn activity)
- [x] 4.5 UI: pending-approval card in the transcript (command text, approve/deny) wired to the resolution endpoint; resolved cards render the eventual tool result
- [x] 4.6 Restart survival test: interrupt, drop the runner, resume from the persisted checkpoint (`TestApprovalFlow_ResumeSurvivesRunnerRestart`)

## 5. Web search multi-provider

- [x] 5.1 `SearchProvider` seam in the tools package: provider interface (query, num → results); the existing DDG scraper extracted as the `duckduckgo` provider unchanged
- [x] 5.2 `tavily` provider: POST to Tavily search API with `ONCLAW_TAVILY_API_KEY`, map results to the same shape; fake-client tests
- [x] 5.3 Instance config: `ONCLAW_SEARCH_PROVIDER` (`duckduckgo` default | `tavily`); construction selects the provider; missing API key for tavily fails tool construction with a clear error

## 6. Browser (go-rod, CDP)

- [x] 6.1 Add `github.com/go-rod/rod`; `BrowserManager` keyed by session ID + jail root — attach to `ONCLAW_BROWSER_CDP_URL` when set, else launcher discovery (CHROME_PATH → PATH → managed download); unavailable → clear tool error; `CloseSession` teardown wired into `streamRun`/`streamResume`
- [x] 6.2 `browser.navigate` and `browser.read` tools (url → title; readable text of current page) with schema docs
- [x] 6.3 `browser.act` tool (click/type/scroll by CSS selector) with schema docs
- [x] 6.4 `browser.screenshot` tool: full-page PNG to `<AgentDir>/browser/screenshot-<seq>.png`, result returns the in-jail path
- [x] 6.5 Register all four in the registry with `ToolContext`-scoped construction; unit tests for schemas, arg validation, session manager lifecycle (fake browser seam); gated integration test behind `ONCLAW_BROWSER_INTEGRATION=1`

## 7. Verification

- [x] 7.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 7.2 Integration suite (`-tags=integration`) against `DATABASE_URL` green — includes migration up/down round-trip (test version assertions now derive from `latestSchemaVersion` = 17)
- [x] 7.3 Grep for stale references: `DisabledTools`, `disabled_tools` — zero hits outside migrations/history
- [x] 7.4 `./scripts/smoke.sh` passes — with one pre-existing caveat: smoke section 13.1 asserts workspace `/skills` CRUD endpoints that were removed from this branch before this change (handlers deleted in the agent-engine consolidation work); that block was skipped for the verification run and everything else, including agent create with tools semantics, passed
