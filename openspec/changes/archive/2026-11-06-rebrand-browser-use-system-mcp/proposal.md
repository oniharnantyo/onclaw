# Proposal

## Why

The browser capability is a built-in tool invisible to the MCP surface, while the product positions integrations as MCP servers. Rebranding it as **Browser Use** — a real, standalone, stdio MCP server that buddies ships, spawns, and consumes itself — makes the "system MCP" story literal, adds crash containment for the app's crashiest dependency (Chromium), removes the need for any localhost socket/token security surface, and lets users bring their existing Chrome logins along via profile import.

## What Changes

- **BREAKING** — the built-in `browser` mega-tool leaves the tool catalog; agents instead see discrete namespaced MCP tools `mcp__browser-use__<tool>` from a system-provided server. Stored per-agent tool allowlists naming `browser` are migrated to the `browser-use` server key.
- Browser state (sessions, janitor, cookie jars, rod) moves out of the app process into a **sole-owner server process**: the same binary invoked as a stdio MCP server subcommand. Disk (`profiles/`) stays the durable state; no IPC, no state fork. Single-owner lockfile guards desktop + server-mode double runs.
- The server exposes **discrete tools** replacing the action enum: `navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, `cookies`, plus new primitives `read` (bounded page text) and `wait` (network-idle/fixed delay); `click`/`type` accept name/role targeting in addition to refs; interactive actions return slim results (no full snapshot dump); navigation settling waits for network quiet.
- The MCP pane gains a locked **System row** ("Browser Use") — spawned built-in server, undeletable, always enabled, status from probe.
- Settings gains a dedicated **Browser Use page** (promoted out of the Tools pane dialog): engine knobs (headless, timeouts, allow-private, default profile), profile registry, and **profile import** — copying an existing Chrome/Chromium user-data-dir profile into buddies' `profiles/`.
- Spawn supervision: respawn-with-backoff, crash-loop cap surfaced as connection status, config changes deferred to idle/respawn boundaries.

## Capabilities

### New Capabilities

- `browser-use`: the Browser Use system MCP server — sole-owner stdio process, discrete tool surface with the new primitives, persistent profiles and import, private-network guard, lifecycle and supervision contract.

### Modified Capabilities

- `agent-tools`: the seven browser requirement blocks (catalog entry, actions, profiles, cookies, private-network guard, lifecycle, settings/profile management) are REMOVED — behavior moves to the `browser-use` capability; the tool gate no longer carries a `browser` catalog entry.
- `agent-mcp`: a system-provided server concept — buddies spawns and supervises a built-in stdio server that appears in the MCP pane as a locked System row and is gated by the standard per-agent server allowlist.
- `settings`: settings navigation gains a dedicated Browser Use pane; the Tools pane browser dialog is removed.

## Impact

- Go: `internal/agents/tools/browser*.go` relocates into a new server package behind a CLI subcommand (pattern: `cli/serve.go`); `buildDirectTool`/gate loses the browser entry; new supervision and lockfile code; allowlist migration for stored agents.
- Frontend: MCP pane System row, new Browser Use settings page, removal of `BrowserConfigDialog` from ToolsPane; tool-card rendering keyed on `mcp__browser-use__*` names; both themes.
- Specs: agent-tools (7 removed requirement blocks), agent-mcp, settings; new browser-use spec.
- No new runtime dependencies (rod stays; mcp-go already vendored). Chromium remains the only external process, now owned by the server child.
