# Proposal

## Why

**buddies** is a rebrand of `onclaw-v2` (a multi-tenant, self-hosted AI agent workspace) into a **local, single-user gateway app** that runs in the background and accepts triggers from cron, webhooks, Telegram, and more. Today the repo is still the bare Wails3 template (`changeme` module, `GreetService`, no domain code). This change lays the foundation: a working desktop app with system-tray background mode, a Wails services API surface shared with a headless server deployment, SQLite persistence, and the three core menus (Chat, Agents, Settings) with working end-to-end chat.

## What Changes

- Rebrand the module to `github.com/oniharnantyo/buddies`; remove template debris (`GreetService`).
- **Dual-host app shell**: one set of bound Wails services serves both hosts — desktop (`main.go`, webview + system tray; closing the window hides it while the gateway keeps running) and server (`main_server.go`, `-tags server` build exposing the same app as a web app over HTTP + the Wails WebSocket bridge, loopback by default with optional bearer token).
- **SQLite persistence** at `~/.buddies/buddies.db` (pure-Go `modernc.org/sqlite`, WAL): normalized config tables (agents, providers, MCP servers, hooks, gateway settings) and event-sourced session tables (`session_events`, checkpoints). Skills remain files on disk under `~/.buddies/skills/` with a DB registry.
- **Agent runtime** on eino (`cloudwego/eino v0.10.0-alpha.35`), ported from onclaw-v2's `internal/agents` and de-tenanted: provider registry, turn execution with token streaming, context-window accounting feeding the UI, compaction.
- **Chat menu**: sessions grouped per agent, chat view with right sidebar, porting onclaw-v2's chat features (context ring, composer with slash/skill/mention menus, tool-call cards, reasoning bubbles, compaction divider).
- **Agents menu**: agent cards (with `react-nice-avatar` avatars) and agent configuration ported from onclaw-v2 (provider/model, system prompt, skills, tools, MCP, hooks).
- **Settings menu**: providers, skills, tools, hooks, MCP panes (workspace/members panes dropped with tenancy).
- Frontend stack additions: daisyUI (light + dark themes), assistant-ui, `react-nice-avatar`.

**Out of scope** (follow-up changes): gateway trigger adapters (cron, webhook, Telegram — this change leaves the gateway service seam only); multi-user and multi-tenant support (permanently out); encryption of secrets at rest; mobile targets.

## Capabilities

### New Capabilities

- `app-shell`: Dual-host application shell — desktop window + system-tray background lifecycle, and headless server mode serving the same frontend as web; the app keeps running when the desktop window is closed.
- `data-store`: SQLite persistence at `~/.buddies` — config tables, event-sourced session history, skills directory, schema migrations.
- `agent-runtime`: Eino-based agent execution — provider registry, streamed turns, context-window accounting, compaction checkpoints.
- `chat-view`: Chat menu — per-agent session list and the chat view with right sidebar and onclaw-v2 chat affordances.
- `agent-management`: Agents menu — agent cards and agent configuration (provider/model, prompt, skills, tools, MCP, hooks).
- `settings`: Settings menu — provider, skills, tools, hooks, and MCP panes.

### Modified Capabilities

(none — no specs exist yet)

## Impact

- **Go**: module rename; new deps `modernc.org/sqlite`, `cloudwego/eino` + `eino-ext` model components (v0.10.0-alpha.35 line), `urfave/cli/v3`; root `main.go` split by build tags (`!server` GUI / `server` headless); new `internal/` packages (`cli`, `config`, `store`, `store/sqlite`, `agents`, `sessions`, `gateway`, `services`); `Taskfile.yml` server targets point at the real binary.
- **Frontend**: new deps daisyUI, `@assistant-ui/react` (+ markdown/generative-ui), `react-nice-avatar`, zustand; new menu structure; ported components from `~/projects/onclaw-v2/web/src` (de-tenanted).
- **Storage**: new on-disk layout `~/.buddies/{buddies.db,skills/,logs/}`.
- **Build**: Docker/server-mode deployment story from the Wails3 template becomes real (`task build:server`, distroless image).
