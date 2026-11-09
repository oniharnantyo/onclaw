# Proposal

## Why

Buddies agents can automate a browser but cannot touch the rest of the machine — native apps, system dialogs, files only reachable through an app's UI. Computer use closes that gap and extends the gateway story (cron, webhooks, Telegram triggers) to full-desktop automation. Both reference implementations solve the OS layer the same way buddies solved browsers: a supervised system MCP process (ZCode via a self-built native helper; OpenBot by vendoring the MIT-licensed `cua-driver` from trycua/cua).

## What Changes

- Add a second system MCP server, `computer-use`, as a sibling of `browser-use`: a supervised `computer-mcp` stdio child (the app's own binary) that adapts a vendored, digest-pinned `cua-driver` daemon into a curated `mcp__computer-use__*` tool surface (~15 tools).
- Vendor the `cua-driver` binary (MIT, notarized, Developer-ID-signed) with a per-release digest pin; downloaded on first enable, vendor signature kept, spawned as a direct child so macOS TCC attributes grants to buddies.
- Generalize the single-system-server registry (`agent-mcp`) to N system servers, with a per-server opt-in flag so adding a system server never silently widens agents whose allowlist is empty.
- Add a permission warm-up gate (macOS first): probes Accessibility + Screen Recording, deep-links System Settings, restarts the driver on grant change, and ships zero tools until grants are ready.
- Serialize all desktop actions through one process-wide dispatcher: a second concurrent caller (e.g. an agent-group member) gets a fixed, non-retryable busy refusal.
- Add an observation image lane: computer-use tool results carry screenshot file markers; a new `observationModel` model decorator injects the pixels as user-role image parts for the most recent markers only (rolling window, gated on per-model image capability).
- Extend the approval seam to a destructive subset of computer-use tools (`kill_app`, clipboard writes, secure-field typing): those park the turn on an approval card before executing.
- Add a Computer Use settings pane (enable toggle, system-permission card with per-permission status / Manage deep links / Check again, driver status) and a second system row in the MCP pane.
- Render computer-use trajectories in chat as one grouped card (vendored assistant-ui computer-use element, restyled to a desktop frame): latest observation screenshot as backdrop, animated cursor over coordinate actions, step footer; `type` text arguments are masked in the UI.
- Gate the whole capability off in server mode (`task run:server`), like the clipboard service.

## Capabilities

### New Capabilities

- `computer-use`: the desktop-control system MCP — vendored driver lifecycle and digest pin, permission warm-up gate, curated tool surface and single-dispatcher arbitration, observation screenshots and the image-marker contract, configuration, and platform limitations.

### Modified Capabilities

- `agent-mcp`: the system-server clause of the allowlist policy generalizes from one hardcoded system server to a registry of N, and an opt-in system server is withheld even when an agent's server list is empty (empty-means-all keeps applying to user servers).
- `chat-approvals`: a destructive subset of computer-use tools parks the turn on the existing approval seam (persisted KindApprovalRequired/Resolved), reusing the shell gate's approve/deny semantics.
- `chat-tool-cards`: computer-use tool calls render as one trajectory card per run of consecutive calls, and the webview obtains computer-use screenshot bytes only through a new bound read jailed to `sessions/<sid>/computer/`.
- `settings`: a Computer Use pane joins the settings nav — enable toggle, system-permission card (status, Manage deep links, Check again), and driver status line.

## Impact

- New Go package `internal/computeruse` (engine + adapter) and `internal/cli/computer_mcp.go` subcommand; supervisor/registry changes in `internal/agents/mcp` (`systemserver.go`, `supervisor.go`, `mcp.go`); approval wrap in `internal/agents/attention.go`; new `internal/agents/observation_model.go` decorator; new `internal/services/settings_computer.go`; screenshot read extended in `internal/services/chat_screenshots.go`.
- New vendored external binary in the trust surface (~53–85 MB per platform), downloaded to `~/.buddies/drivers/` with digest verification; new env-var contract with the driver (`DO_NOT_TRACK=1`, `CUA_DRIVER_RS_*`).
- Frontend: new `ComputerUsePane.tsx`, `ComputerTool.tsx` trajectory renderer (vendored assistant-ui element copy), McpPane/toolDisplay registry updates, new `computerScreenshot` client binding.
- No store schema migrations (config rides the `tool_settings` document; opt-in is code, not data).
- New dependency class: an OS-level automation binary with platform edges (primary display only on macOS multi-monitor, Windows elevated targets refused, Wayland compositor-by-compositor) — documented limitations, not blockers.
