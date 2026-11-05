# Design

## Context

Today the browser capability is one built-in catalog tool (`internal/agents/tools/browser*.go`): a 9-action enum over rod + CDP, in-process with the app. State lives in a process-wide `browserManager` singleton (live sessions per profile, idle janitor, cookie-jar export/inject at teardown/launch). Profiles are disk-backed under `<browserDir>/profiles/<name>/` and already survive restarts — disk is the durable state; the live session map is reconstructible on demand. Config resolves lazily per call via `BrowserConfigSource`; the frontend surfaces it as `BrowserConfigDialog` inside ToolsPane.

MCP client machinery (mcp-go v0.43.0 client) already handles stdio/HTTP/SSE servers, probe + discovery, `mcp__<server>__<tool>` naming, per-agent server allowlists (default: every enabled server), and the MCP pane. A CLI subcommand pattern exists (`cli/serve.go`). See proposal.md — Why.

## Goals / Non-Goals

**Goals:**
- Browser Use runs as a real stdio MCP server: a separate sole-owner process buddies spawns from its own binary.
- Browser state keeps a single owner (the server process); `profiles/` on disk stays the durable state.
- Agents consume browser tools through the standard MCP path — naming, discovery, allowlists, pane status.
- Crash containment: Chromium/rod failures kill the child, not the app.
- Profile import from an existing Chrome/Chromium user-data dir.
- Primitive upgrades ride along: discrete tools, name/role targeting, `read`, `wait`, slim results, network-quiet settling.

**Non-Goals:**
- External exposure of the server beyond buddies' own spawn (a "connect from Claude Desktop" story) — the binary will technically support it, but no UI, docs, or profiles-dir UX ships in this change.
- Any new browser engine (Stagehand, browser-use CLI) — rod stays.
- Per-call config push to a running server; config changes apply at respawn boundaries.
- Renaming the `browser` tool for any transitional period — the old catalog entry is removed outright, with an allowlist migration.

## Decisions

**D1 — Separate sole-owner process over in-process loopback.** The server is a child process speaking stdio, and it is the ONLY owner of live browser state. Rejected alternative: in-process mcp-go server on `127.0.0.1:<random port>` with a per-boot token. Loopback keeps the refactor smaller but requires a network endpoint protectable against other local processes (a logged-in-profile driver is a juicy local target), adds a port/token lifecycle, and loses crash containment. Stdio has no socket at all — the parent-child pipe pair is the entire transport — and a child crash is containable by respawn. Disk-backed profiles make sole ownership cheap: nothing in the app needs live access to browser state.

**D2 — Same binary, subcommand entrypoint.** The server is the buddies binary invoked as `buddies browser-mcp` (CLI pattern per `cli/serve.go`), located via `os.Executable()`. The child exits on stdin EOF — the standard MCP stdio contract — so parent death cleans up without platform-specific job objects. Dev mode (`task dev`) works because the watcher-restarted parent's new child re-spawns; a dead parent closes the pipe and the old child exits.

**D3 — State moves wholesale; no IPC.** `browserManager`, `profileSession`, cookie-jar export/inject, the janitor, and the rod launcher relocate into the server package. The app process retains zero browser code. The shared durable state is `<browserDir>/profiles/` exactly as today; sessions re-open on demand and jars re-inject on launch, so a child restart after crash is the already-implemented crash-recovery path. No IPC channel exists or is needed.

**D4 — Single-owner lockfile.** A lock file in `<browserDir>/` (created with `O_CREATE|O_EXCL`, holding pid + timestamp; stale — pid dead or older than a threshold — is broken and retaken, mirroring the existing stale-SingletonLock dance) guards against desktop + server-mode double runs sharing `~/.buddies`. A second server instance fails startup with a clear reason, which surfaces in the MCP pane connection status. The lock lives with the profiles dir, not the app, so alternate browser dirs (tests, portable setups) lock independently.

**D5 — Discrete tool surface.** The 9-action enum becomes 9 discrete MCP tools (`navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, `cookies` — cookies keeps its `op` argument) plus two new tools: `read` (bounded page text extraction, e.g. 20k chars cap) and `wait` (network-idle or fixed delay). Each tool gets its own JSON schema and carries the current `Info()` description text. Upgrades folded into the same surface, since every tool is being redefined anyway:
- `click`/`type` accept `name`/`role` targeting in addition to `ref`: a deterministic case-insensitive match against the current AX snapshot (prefer exact name match, then role+substring), erroring with the candidate list when ambiguous — "click the Sign in button" becomes one round trip with no snapshot bookkeeping.
- Interactive actions return **slim results**: `{ok, url, title, changed: [...small element deltas...]}` instead of a full snapshot; full element lists ride only `snapshot`/`read`.
- Navigation settling waits for network quiet (rod `WaitRequestIdle`) with the existing `WaitStable` fallback for load-less SPAs.
Refs remain per-snapshot and reset on navigate, as today.

**D6 — Naming, gating, and the System row.** Tools surface as `mcp__browser-use__<tool>`. The system server is synthesized in the MCP client manager as a built-in definition (transport stdio, command = own binary + subcommand), never stored as an `mcp_servers` row — undeletable and always-enabled by construction, and it participates in the standard per-agent server allowlist (default-on, preserving today's default-on posture for the browser). Stored per-agent tool allowlists referencing `browser` migrate once at load: a `browser` entry maps to the `browser-use` server key (pinned→pinned, excluded→excluded). Rejected alternative: keep the old tool name during a transition window — two surfaces for one capability is worse than one clean migration.

**D7 — Supervision.** The client manager spawns the child at manager init and owns the lifecycle: on child exit, respawn with exponential backoff (e.g. 1s → 30s cap); after N consecutive failures (e.g. 5), stop respawning and surface a failed connection status with the last stderr excerpt — the standard probe-failure UI. A successful probe resets the counter. Rejected: supervisor as a separate daemon — one more process with no added value; the client manager already owns connection state.

**D8 — Config handoff.** A config snapshot (headless, idle timeout, action timeout, allow-private, default profile, browser dir) is passed at spawn via command-line flags. Action timeouts are enforced inside the server per call from the snapshot, as today. Config changes in settings persist, then take effect at the next spawn boundary: the supervisor respawns only when no session has live pages, otherwise defers until the janitor next reaches zero live sessions (the janitor already ticks every 30s). Live sessions are never killed mid-action to apply config — the accepted cost is that a config change may wait for idle.

**D9 — Profile import.** The Browser Use settings page gains an import action: user picks an existing Chrome/Chromium user-data directory (or a profile subdirectory within one); the app copies its contents into `profiles/<imported-name>/` (name validated by the existing profile-name contract; collision appends a suffix). Copy, never move; the source is untouched. UI copy states the requirements honestly: close the source Chrome first (its SingletonLock would fight the copy), expect a large copy for long-lived profiles, and cookie decryption is best-effort — it works when the copy stays on the same machine and browser (macOS Keychain / Linux), and may not survive Windows App-Bound Encryption (Chrome 127+); logins via cookie jar reinjection still cover session cookies the jar captured. An imported profile is thereafter a normal profile: janitor, lockfile, jar export/inject all apply.

**D10 — Guards and bounds carried over unchanged.** The private-network guard (http/https only, resolve-and-block non-public addresses, post-redirect re-check with about:blank reset), the 150-element snapshot bound with truncated names, and the screenshot-to-disk pattern (file under `<browserDir>/screenshots/`, path returned) move into the server tools verbatim. `read` output is bounded and truncated mid-page rather than failing.

**D11 — Frontend surfaces.** New Browser Use pane registered in the settings navigation catalog (page, not dialog): engine knobs, profile registry (create/delete/import), server status line fed by probe state. MCP pane renders the locked System row from the client manager's built-in definition (badge distinct from user rows; no edit/delete affordances; "Open settings" → the new pane). `BrowserConfigDialog` and its ToolsPane hook are removed. Tool cards and any name-keyed chat surfaces update to the `mcp__browser-use__*` names. All new UI obeys the two-theme contract (tokens.css light/dark).

**D12 — Execution parity.** Browser-use tools ride the standard MCP tool-execution path (`agent-mcp`), inheriting whatever error/timeout semantics MCP tools already get; no browser-specific approval surface is added or removed relative to today's built-in (the built-in has none). Tool-error results keep turns alive as MCP errors already do.

## Risks / Trade-offs

- [Respawn drops live sessions] → config changes and crash recovery only restart at idle boundaries (D7/D8); crash respawn is the rare path and recovers profiles from disk.
- [Respawn storm on a broken child] → backoff + consecutive-failure cap surfacing as failed status (D7).
- [Double-run across desktop + server mode] → browser-dir lockfile with stale-breaking, failure surfaced as connection status (D4).
- [Imported profile cookies undecryptable (Windows ABE, cross-machine)] → import is explicitly best-effort in UI copy; jar-based session cookies still carry logins the browser had live (D9).
- [Self-spawn fragility in dev] → same-binary subcommand + stdin-EOF exit; watcher restarts exercise the respawn path continuously in `task dev`.
- [Allowlist/tool-name migration misses] → migration is keyed on the exact old name `browser` at settings load; smoke includes an agent with a stored `browser` pin.
- [First browser action pays child spawn + MCP initialize] → supervisor spawns at manager init, long before first use; rod launch dominates either way.

## Migration Plan

1. Ship behind no flag — the migration is self-contained: settings load maps stored `browser` allowlist entries to the `browser-use` server key (idempotent).
2. `profiles/` layout, jar files, and screenshot dirs are unchanged — disk state is compatible in both directions; rollback is reverting the commit.
3. Frontend keys update in the same change; no persisted frontend state references tool names except chat transcripts, which are historical records (old entries render as generic tool cards).

## Open Questions

None blocking. External-serve UX (profiles dir selection for out-of-app clients) is deferred per Non-Goals.
