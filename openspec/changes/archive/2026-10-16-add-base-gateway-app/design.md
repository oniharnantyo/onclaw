# Design

## Context

buddies is currently the bare Wails3 template (module `changeme`, `GreetService`, template React app) with `task build:server`/`run:server` stubs pointing at a `-tags server` binary that does not exist yet. The donor codebase `onclaw-v2` (local repo, sibling directory) supplies the domain code to port: eino-based `internal/agents`, registry-pattern providers/skills/tools/hooks/MCP, event-sourced session model (`SessionEvent`/`SessionCheckpoint`), and a large React chat/agent/settings frontend (assistant-ui + zustand + Tailwind). Wails3 beta.29 (pinned) ships the two features this design leans on: system tray, and server mode (`-tags server`, `Options.Server`, WebSocket runtime bridge with per-browser-tab `BrowserWindow` and a `Stream()` binding transport). Verified constraints from `openspec/config.yaml`: daisyUI with light+dark themes for every UI surface; assistant-ui for chat.

## Goals / Non-Goals

**Goals:**

- One service API surface (bound Wails services) consumed identically by the webview and the browser.
- Core engine (agents, sessions, config, gateway seam) importable without Wails.
- All state in SQLite under `~/.buddies`; no external database.
- Desktop closes to tray and keeps running; server mode deploys as web.
- Chat/agent/settings menus ported from onclaw-v2 with working end-to-end chat on one wired provider path.

**Non-Goals:**

- Gateway trigger adapters (cron, webhook, Telegram) — only the gateway service seam and lifecycle land here.
- Multi-user, multi-tenant, or network-hardened auth (loopback default + optional bearer token is the whole security story for base).
- Secrets encryption at rest.
- Public REST API for external consumers.

## Decisions

### D1: Wails services + bindings are the only API surface (no REST layer)

The frontend talks exclusively through generated TypeScript bindings (`Call`, `Events.On`, `Stream`) from `@wailsio/runtime`; in the webview this is the native bridge, in server mode it is the WebSocket bridge. onclaw-v2's REST handler layer is **not** ported.

- *Why*: beta.29's server mode makes "same bindings, two hosts" a supported path, eliminating an entire REST handler + DTO layer that would exist only to mirror the services. Streaming uses the built-in `Stream()` transport instead of fetch-SSE through the asset server (the previous plan's main technical risk).
- *Alternative considered*: port onclaw-v2's gin REST API and mount it under Wails' `AssetOptions.Handler`. Rejected: duplicates the service layer, requires a same-origin SSE spike through the webview asset chain, and the curl-ability benefit matters only for a public API that is a non-goal.
- *Insurance*: the core (`internal/agents`, `internal/sessions`, `internal/config`, gateway seam) MUST NOT import Wails packages; services in `internal/services` are thin wrappers. A REST/WS host can be added later without touching the core.

### D2: SQLite via `modernc.org/sqlite` at `~/.buddies/buddies.db`

Pure-Go driver (no CGO) keeps the server build static (distroless Docker story from the template) and avoids CGO on desktop/mobile targets. WAL mode + busy timeout for concurrent access from UI calls and background services. Embedded forward-only migrations run at startup before services accept calls. Store layer follows onclaw-v2's pattern: per-aggregate sub-interfaces (`AgentStore`, `SessionEventStore`, …) with a `store/sqlite` adapter; test fakes mirror onclaw-v2's in-memory fakes.

- *Alternatives*: JSON config + JSONL sessions (original brief, superseded by user decision — loses atomic config edits and queryability); `mattn/go-sqlite3` (CGO, breaks static server builds); Postgres (external dependency, contradicts single-binary local app).

### D3: Configuration lives in the database; skills live on disk

Agents, providers, MCP servers, hooks, gateway settings are normalized tables. Skills remain markdown directories under `~/.buddies/skills/` (ecosystem-native shape, matches onclaw-v2's skill curation model) with a registry table indexing them. Provider API keys are stored in the clear for base (single-user local assumption; encryption is a later change).

### D4: Root main split by build tags, composition root in `internal/cli`

`main.go` (`//go:build !server`) = desktop: window + tray, `ApplicationShouldTerminateAfterLastWindowClosed: false`, hide-on-close, tray menu (Open / gateway state + pause-resume / Quit — Quit is the only full exit). `main_server.go` (`//go:build server`) = headless: `Options.Server` bound to loopback by default, optional bearer token. Both assemble the same services through `internal/cli` (urfave/cli/v3, onclaw-v2's composition-root pattern; commands: default GUI launch, `serve`, `version`). Module renamed to `github.com/oniharnantyo/buddies`.

- *Why build tags over two cmd/ binaries*: the Wails toolchain and template Taskfiles conventionally build the root package; `-tags server` is the template's own existing seam.

### D5: Agent runtime ported from onclaw-v2 on eino v0.10.0-alpha.35

Port `internal/agents` core (manager, turn execution, context accounting, compaction, provider registry) de-tenanted: workspace/tenant parameters collapse to nothing; single implicit owner. Session persistence maps `SessionEvent` → `session_events` rows (JSON payload column) and `SessionCheckpoint` → checkpoints table. Base wires one provider path end-to-end (Anthropic via `eino-ext/components/model/claude` — the path onclaw-v2 exercises most); other provider adapters register but are not required to pass verification.

### D6: Frontend = onclaw-v2 components with swapped transport

Port `web/src` components (ChatView, RightPanel, ContextRing, Composer, Slash/Skill/Mention menus, ToolCall, ReasoningBubble, CompactionDivider, AgentsView/AgentCard, AgentConfigModal sections, Settings panes minus workspace/members) into `frontend/src`. The REST transport (`lib/api.ts`, fetch-SSE in `livechat.ts`) is **rewritten** onto generated bindings; components and zustand stores are largely unchanged. Styling moves to daisyUI with light+dark themes (per project config; replaces raw Tailwind class conventions where needed). Avatars: `react-nice-avatar`, deterministically seeded from agent identity. One bundle embedded once serves both hosts.

### D7: Turn streaming over events + Stream transport

`SendMessage` accepts a message and returns immediately; turn progress (text deltas, reasoning, tool activity, completion, errors) flows as Wails events scoped to the session, and the `Stream()` transport carries token deltas. Transcripts persist server-side per turn regardless of client connectivity (spec: disconnected client does not lose the turn).

## Risks / Trade-offs

- [beta.29 server mode is young (WS bridge, per-tab BrowserWindow, Stream transport)] → first implementation task is a throwaway spike: server-mode build, two browser tabs, event broadcast, stream, reconnect. If it fails, the documented fallback is D1's alternative (REST host over the same wails-free core) — costly but not architectural rework.
- [eino is an alpha release line] → pin exactly `v0.10.0-alpha.35` (user decision); wrap eino types behind the runtime package boundary so upgrades stay local.
- [Porting the 2,475-line AgentConfigModal and chat surface is the bulk of the effort] → port sections as vertical slices per menu; components before transport polish; de-tenancy means deleting workspace/user plumbing, not rewriting it.
- [No curl-able API complicates debugging] → bound services remain plain Go methods with unit tests against fakes; `wails3 doctor`/dev tooling covers binding issues.
- [WAL + two hosts writing] → busy_timeout with retry on the store adapter; server mode documented as single-instance per data directory.
- [Plaintext credentials in DB] → acceptable for base per proposal; document `~/.buddies` permission expectations (0700) in tasks.

## Migration Plan

No existing users or data — fresh-install semantics only. Sequence: module rename and template debris removal first (keeps `task dev` green), then store + services behind fakes, then GUI wiring, then server mode. Rollback = `git revert`; no data migrations to unwind. On-disk layout (`~/.buddies`) is created idempotently, so reinstall/rollback is safe.

## Open Questions

- Exact skill file format to accept in `~/.buddies/skills/` — align with onclaw-v2's skill markdown conventions during implementation; does not change specs.
- Server-mode token delivery for the WebSocket handshake (header vs first-message auth) — spike will determine; spec only requires rejection without token.
