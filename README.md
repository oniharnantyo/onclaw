# buddies

A local, single-user AI agent gateway. buddies runs as a background desktop app
(system tray) or a headless server, hosts configurable agents on the
[eino](https://github.com/cloudwego/eino) runtime, and persists everything in a
local SQLite database — no external services required.

- **Desktop**: window + system tray. Closing the window hides it; the gateway
  keeps running. Quit from the tray is the only full exit.
- **Server mode**: the same app served as a web app over HTTP/WebSocket,
  bound to loopback by default, with an optional bearer token.
- **Menus**: Chat (every agent's sessions grouped in a collapsible rail —
  avatar, name, and session count per group, with the active agent's group
  expanded — plus streaming transcript, context usage, compaction), Agents
  (cards + full configuration), Settings (providers, skills, tools, hooks,
  MCP servers).

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

## Webhooks

Webhooks are the third inbound trigger surface, next to cron schedules and
Telegram chat: external systems (GitLab, GitHub, Stripe, CI, automation
tools) start agent or group work by POSTing an HTTP webhook to the running
gateway. Endpoints are managed in Settings > Channels > Webhooks; each
endpoint pairs a unique name with a target — an agent or an agent group — a
prompt template, an enabled flag, and at least one credential, and gets a
delivery URL on the webhook listener (`POST /webhooks/<endpoint id>`,
loopback `127.0.0.1:8787` by default).

The listener follows the gateway lifecycle: while the gateway is Running it
serves deliveries; while Paused it answers `503` with a `Retry-After` hint
(the sender's retry policy is the queue); when stopped, or on app close, it
shuts down with a bounded teardown. The listen address is configurable in
the same section and applies without a restart, and the listener behaves
identically on desktop and server hosts.

A delivery to an agent target fires a fresh titled session per delivery
(`<endpoint name> — <delivery time>`), submitting the prompt template with
the request body appended as a fenced payload block (`json`-fenced when the
body parses as JSON, plain-fenced otherwise). The turn is stamped with the
webhook origin so hooks treat it as an autonomous run, and its completion
raises a desktop notification that routes to the session. A delivery to a
group target appends the rendered text as an ordinary user message in the
group's transcript — mentions route to named members, untargeted text to
the lead.

Credentials use five verifier engines — URL path token, static header
equality, raw-body HMAC, Standard Webhooks HMAC-SHA256 (with a
`webhook-timestamp` replay window), and timestamp-composite HMAC (the
Stripe and Slack/Zoom compositions) — with one-secret-field presets for
GitLab, GitHub, Stripe, Slack, Zoom, Shopify, Linear, and plain header
senders, plus a manual builder over the raw engines. Any verifier pass
authenticates a delivery; a request carrying an expected signature header
that fails verification is refused `401` with no fall-through to weaker
credentials. Secrets are write-only: shown once at creation or
regeneration, never returned by reads, and a blank secret save keeps the
stored one.

Every delivery attempt lands in a bounded per-endpoint log (last 50):
HTTP status, outcome, the session or transcript message it produced, the
verifier that authenticated it, and the sender's `webhook-id` when present
— a repeat delivery with the same `webhook-id` answers `200 duplicate`
without firing twice. Accepted deliveries answer `202`; bodies beyond
1 MiB answer `413`; unknown or disabled routes answer `404`. The Test
button composes and self-signs a delivery through the real listener, so
address binding, credentials, and dispatch are proven in one step (logged
as `self-test`).

## Data layout

All app data lives under `~/.buddies` (created on first run, permissions 0700):

| Path | Purpose |
|---|---|
| `~/.buddies/buddies.db` | SQLite (WAL): agents, providers, MCP servers, hooks, gateway settings, event-sourced session history, checkpoints, skill/tool registries |
| `~/.buddies/skills/` | Skill directories (each a folder with a `SKILL.md`), indexed into the DB registry |
| `~/.buddies/workspaces/<slug>/memory/` | Per-agent memory: curated index `MEMORY.md`, fact notes `<name>.md`, daily notes `YYYY-MM-DD.md`, and `archive/` |
| `~/.buddies/logs/` | Application logs |

Set `HOME` to a scratch directory to run against an isolated data dir.

## Memory

Each agent keeps durable memory as plain Markdown in its workspace at
`memory/`, created on demand: a curated index (`MEMORY.md`, a free section
plus a sweep-managed section), atomic fact notes (`memory/<name>.md`), daily
working notes (`memory/YYYY-MM-DD.md`), and an `archive/` subdirectory. Fact
notes carry a frontmatter block: `name` (the filename slug), `description`,
`type` (one of `user`, `feedback`, `project`, `reference`), optional
`concepts`, `relations` (`has_property`, `prefers`, `related_to`,
`contradicts`, `causes`, `part_of`), `supersedes` (the note name it
replaces), and `[[wiki-links]]` in the body. Agents curate memory during
their own turns with the workspace file tools; a seeded global `memory` skill
(invocable as `$memory`, shadowable per agent) carries the read-first recall
playbook: consult the index, run `memory_search`, read the note.

The `memory_search` tool runs lexical search over fact-note frontmatter and
bodies, daily notes, and the index, returning ranked results with name,
description, path, score, and matched excerpt. It makes no model call; every
invocation records a recall trace, and only user-origin traces feed
consolidation.

A consolidation sweep runs per agent once an hour while the gateway is
Running (skipped while Paused) and makes no model call: it scores candidates
from recall signals, promotes qualifying daily-note lines into the index
append-only, updates note retention, and moves stale notes to
`memory/archive/` — archived, never deleted; moving a note back restores it.
Every turn composes a budgeted memory block (the index plus top notes) after
the operating contract and before the compaction summary.

## Project layout

- `main.go` / `main_server.go` — desktop and server hosts (build tags `!server` / `server`)
- `internal/cli` — composition root (urfave/cli); assembles config → store → runtime → services for both hosts
- `internal/config` — `~/.buddies` path resolution and first-run layout
- `internal/store` (+ `store/sqlite`, `store/fake`) — persistence ports, SQLite adapter (embedded migrations), in-memory fakes
- `internal/agents` — eino agent runtime (turn execution, streaming, context accounting, compaction, provider registry, built-in tools); eino types stay inside
- `internal/memory` — file-backed agent memory: workspace layout, frontmatter notes, lexical index, recall traces, and the hourly consolidation sweep
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
