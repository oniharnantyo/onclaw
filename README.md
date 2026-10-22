# buddies

A local, single-user AI agent gateway. buddies runs as a background desktop app
(system tray) or a headless server, hosts configurable agents on the
[eino](https://github.com/cloudwego/eino) runtime, and persists everything in a
local SQLite database — no external services required.

- **Desktop**: window + system tray. Closing the window hides it; the gateway
  keeps running. Quit from the tray is the only full exit.
- **Server mode**: the same app served as a web app over HTTP/WebSocket,
  bound to loopback by default, with an optional bearer token.
- **Menus**: Chat (per-agent sessions, streaming transcript, context usage,
  compaction), Agents (cards + full configuration), Settings (providers,
  skills, tools, hooks, MCP servers).

## Requirements

- Go 1.26+ and [Task](https://taskfile.dev) (`go install github.com/go-task/task/v3/cmd/task@latest`)
- The [wails3](https://v3.wails.io/) CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`)
- Node.js 20+ with npm 11+ (frontend build)

## Development

```sh
task dev          # desktop app with hot reload (frontend + Go)
```

## Desktop build & run

```sh
task build        # production desktop binary → bin/buddies
task run          # build and launch the desktop app
```

On macOS the desktop binary is `bin/buddies` (wrapped in `bin/buddies.app` by
the packaging task). The tray menu offers Open, gateway pause/resume, and Quit.

## Server mode

```sh
task build:server         # headless binary → bin/buddies-server
bin/buddies-server        # serves on 127.0.0.1:8080, no auth (local single-user default)
```

With a bind address and bearer token:

```sh
bin/buddies-server serve -- --addr 127.0.0.1:8080 --token s3cret
# equivalent env form:
BUDDIES_ADDR=127.0.0.1:8080 BUDDIES_TOKEN=s3cret bin/buddies-server
```

When a token is configured, every request and WebSocket connection must present
it via `Authorization: Bearer <token>` (browsers/EventSource may use
`?token=<token>`, which is stripped before proxying). Without a token, no auth
is required. The server binds loopback unless explicitly told otherwise.

> macOS note: the server binary links Cocoa/WebKit due to a Wails beta.29
> build-tag gap, but runs fully headless. The Linux build (and the Docker
> image) are genuinely GUI-free.

## Data layout

All app data lives under `~/.buddies` (created on first run, permissions 0700):

| Path | Purpose |
|---|---|
| `~/.buddies/buddies.db` | SQLite (WAL): agents, providers, MCP servers, hooks, gateway settings, event-sourced session history, checkpoints, skill/tool registries |
| `~/.buddies/skills/` | Skill directories (each a folder with a `SKILL.md`), indexed into the DB registry |
| `~/.buddies/logs/` | Application logs |

Set `HOME` to a scratch directory to run against an isolated data dir.

## Project layout

- `main.go` / `main_server.go` — desktop and server hosts (build tags `!server` / `server`)
- `internal/cli` — composition root (urfave/cli); assembles config → store → runtime → services for both hosts
- `internal/config` — `~/.buddies` path resolution and first-run layout
- `internal/store` (+ `store/sqlite`, `store/fake`) — persistence ports, SQLite adapter (embedded migrations), in-memory fakes
- `internal/agents` — eino agent runtime (turn execution, streaming, context accounting, compaction, provider registry, built-in tools); eino types stay inside
- `internal/sessions` — event-sourced session model
- `internal/gateway` — background lifecycle seam (state, pause/resume)
- `internal/services` — thin Wails-bound services (the only API surface)
- `internal/skills` — skills directory scanner
- `frontend/` — React + Tailwind v4 on the onclaw-v2 token contract (`design.md`; light/dark themes) + zustand; generated Wails bindings in `frontend/bindings/`

## Verification

```sh
gofmt -w . && go vet ./... && go test -race ./...
cd frontend && npm run test && npm run build
```
