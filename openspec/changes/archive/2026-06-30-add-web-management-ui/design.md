## Context

onclaw is a CLI-only agent for low-resource SBCs (~2 GB RAM, 8 GB storage, ARM).
Management today is SSH + subcommands. The conversation-history store
(`internal/store/sqlite/conversation.go`) is new and has no list/browse path.
This change adds a browser console served by the binary.

Reuse points (verified against the code):
- **Assembly**: `internal/cli/context.go:47` `getProviderManager(c) (*llm.Service,
  *sql.DB, error)` opens SQLite, decrypts the DEK (keyfile mode), and assembles
  `llm.NewService(...)`. Reusable as-is for a long-lived server.
- **Hot-reload**: `writePIDFile` + `signalRunningProcess` (`context.go:145,158`)
  and `llm.StartDBWatcher` (`internal/llm/watcher.go`) + SIGHUP set
  `Service.reloadPending`. Same wiring `run.go` uses.
- **CRUD**: `llm.Service` already exposes providers, secrets, and agents.
- **Run + stream**: `agent.AssembleAgent(...)` (`internal/agent/agent.go:30`)
  builds the agent; `(*Agent).Run(ctx, input)` (`internal/agent/agent.go:203`)
  returns an `EventIterator` (`internal/agent/event_iterator.go`) that yields
  `*schema.AgenticMessage` values by draining the model's `*schema.StreamReader`
  (`internal/agent/iterator.go`), so output arrives incrementally. A
  `render.Renderer` (`internal/render`) renders messages to bytes for the CLI.
- **Model build**: `mgr.BuildWithProfile` returns a `model.AgenticModel` via the
  per-provider `agentic_*` adapters (`internal/llm/adapter`); `AssembleAgent`
  consumes it.
- **Session helper**: `internal/cli/agent_session.go` `resolveAndAssemble(...)`
  gathers agent -> workspace -> provider -> profile -> model -> reasoning ->
  context window -> assemble; `serve` reuses it.
- **Embedding**: `//go:embed` is already used at `internal/agent/embed.go:10`.

This change builds on the agentic-message migration (archived
`2026-06-30-migrate-agent-to-agentic-message`): it consumes `(*Agent).Run`'s
`EventIterator`, not the removed `RunAgent` function.

The codebase follows interface/types/implementation separation and prefers formal
store methods over direct SQL.

## Goals / Non-Goals

**Goals:**
- Serve a browser UI from the binary, **client-rendered (no SSR)**, so the device
  does no HTML work and memory is spared.
- Reuse the existing service/store layer + hot-reload; **no new Go dependencies**
  (stdlib `net/http` + `encoding/json` + bcrypt).
- Cover providers/secrets, agents, conversation history, and live streaming chat.
- Gate LAN access behind a passphrase login.

**Non-Goals:**
- Server-side rendering or Go/html templates (deliberately avoided).
- Multi-user auth, RBAC, or horizontal scale (single-user, in-memory sessions).
- A mobile-native app.
- Supporting Argon2id passphrase DEK unlock in `serve` for v1 (keyfile mode only,
  matching `run`/`chat`).
- Changing provider, agent, or conversation semantics.

## Decisions

### 1. Client-rendered SPA, no SSR

**Decision:** the device serves static assets + JSON only; the browser renders
everything. The React app calls a JSON API.

**Rationale:** shifts all HTML work to the (far more powerful) client browser,
keeps the device free of per-request template rendering, and yields a clean,
reusable JSON API. This also removes any need for a Go templating layer.

### 2. Go side is a pure JSON API + static host

**Decision:** new `internal/web/` package using stdlib `net/http` (no framework,
no new Go module deps). Handlers marshal DTOs with `encoding/json`; a non-`/api`,
non-asset GET falls through to `index.html` for client-side routing. No new Go
dependencies are introduced.

### 3. React SPA embedded via go:embed

**Decision:** a Vite + TypeScript React app lives in a repo-root `web/` directory
and builds into `internal/web/assets/` (Vite `outDir`), embedded with
`//go:embed`. `//go:embed` cannot use `..`, so the build output must land inside
the embedding package.

**Design system (Cold devtool):** shadcn/ui (owned, customized) + Tailwind v4 +
TanStack Table + Phosphor icons + self-hosted Geist Sans/Mono. Dark-first Zinc
base with one electric-blue accent (no AI-purple); mono for model names, IDs,
counts, and timestamps; a single radius scale; functional motion only.

### 4. Reuse the assembly root and hot-reload

**Decision:** `onclaw serve` calls `getProviderManager(c)`, writes the PID file,
starts `StartDBWatcher`, and installs a SIGHUP handler that calls
`mgr.TriggerReload()`, exactly as `run.go` does. It also reuses the
`resolveAndAssemble` helper (`internal/cli/agent_session.go`) to gather the
session, mirroring `run.go`'s recipe (PID, SIGHUP, watcher, create conversation,
assemble, iterate) but rendering to SSE instead of stdout and staying long-lived.
Edits made through the UI therefore hot-reload into any running
`serve`/`run`/`chat` process.

### 5. Streaming chat over SSE via the EventIterator

**Decision:** `POST /api/chat` reuses `resolveAndAssemble` to build the agent
(same path as `run.go`), creates/reuses a conversation, then calls
`assembledAgent.Run(ctx, prompt)` and loops the returned `EventIterator`. Each
`Next()` yields a `*schema.AgenticMessage`; the iterator drains the model's
`*schema.StreamReader`, so output arrives incrementally (including token deltas
when the model streams). The handler serializes each message as an SSE event,
ending with `event: done` or `event: error`. It honors `r.Context()` so a client
disconnect cancels the run (the iterator already observes `ctx`). **No agent
changes are needed for v1.** Messages persist automatically through the existing
history middleware. The handler reuses the `render.Renderer` abstraction (or a
small SSE serializer alongside `internal/render`) rather than reaching past the
iterator.

### 6. Passphrase login, independent of the DEK

**Decision:** the web passphrase is stored **bcrypt-hashed** in the `preferences`
KV (`web_password_hash`), set via `onclaw serve --set-password`. `serve` refuses
to start with no hash configured. Sessions are in-memory random tokens (24h) in a
signed `HttpOnly`, `SameSite=Strict` cookie; CSRF is handled with an
`Origin`/`Referer` check on state-changing requests.

**DEK interplay:** the web login is app-level auth, separate from data-at-rest
encryption. `serve` decrypts the DEK via the existing keyfile-mode path
(unattended), identical to `run`/`chat`. v1 does not support Argon2id passphrase
DEK unlock for `serve`.

### 7. The only store change is `ListConversations`

**Decision:** add `ConversationStore.ListConversations(ctx) ([]*ConversationRow,
error)` plus a `ConversationRow{ID, AgentName, CreatedAt, UpdatedAt, MessageCount}`
DTO. No other store or schema changes; `web_password_hash` reuses the existing
`preferences` KV.

## Risks / Trade-offs

- **Node toolchain in CI/dev only.** `make ui` requires Node; the device never
  does. A committed fallback `index.html` keeps the Go build green without the UI,
  and CI must run `make ui` before `build-all`.
- **Event granularity (Low).** The `EventIterator` yields `*schema.AgenticMessage`
  values that mix assistant deltas, tool calls, and tool results; v1 emits each as
  an SSE event and lets the client render them. A typed event vocabulary
  (assistant-text / tool-call / tool-result) can be layered on later without
  agent changes.
- **Bundle size.** ~200-300 KB gzipped first load, all client-side (device
  unaffected).
- **In-memory sessions.** Lost on restart, requiring re-login. Acceptable for a
  single-user console.

## Migration Plan

Spec-only at this stage. Implementation milestones (M0 scaffold through M4 live
chat) are tracked in `tasks.md`. No database migration; `web_password_hash`
reuses the `preferences` KV.