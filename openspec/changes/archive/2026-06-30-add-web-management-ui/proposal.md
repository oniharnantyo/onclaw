## Why

onclaw is CLI-only today. Every management action (providers, agents, secrets,
conversations) happens over SSH and subcommands, and the brand-new
conversation-history store has **no way to be listed or browsed at all**. For a
device you reach from a phone or laptop on the LAN, a lightweight browser
console is the natural way to manage the agent and inspect runs without SSH.

The codebase is already UI-ready at the data layer: every entity sits behind the
`llm.Service` / `store` facades with a working hot-reload mechanism, and the
agent now exposes a headless `(*Agent).Run` iterator over agentic messages. The
UI is a thin layer over those existing methods.

## What Changes

- **`onclaw serve`**: a long-lived command that reuses `getProviderManager()` for
  assembly, writes the PID file, starts the DB watcher + SIGHUP handler for
  hot-reload (same wiring as `run.go`), and serves an embedded web console.
- **Pure JSON API** (stdlib `net/http`, no framework, no new Go deps) under a new
  `internal/web/` package: providers, agents, masked secrets, conversations, and a
  streaming chat endpoint, all delegating to existing `llm.Service` / store
  methods. **No server-side HTML rendering**; the device serves static assets and
  JSON only.
- **Embedded client-rendered React SPA** (Vite + TypeScript), built into static
  assets and embedded via `//go:embed`. Cold-devtool design system: shadcn/ui +
  Tailwind v4 + TanStack Table + Phosphor + Geist, sidebar nav, providers/agents
  CRUD, conversation viewer, and live chat over SSE.
- **Passphrase login**: a bcrypt-hashed web password in the `preferences` KV
  (separate from the DEK), set via `onclaw serve --set-password`, gating every API
  route except `/api/login`.
- **Streaming chat over SSE**: the server calls `(*Agent).Run`, which returns an
  `EventIterator` that yields `*schema.AgenticMessage` chunks (it drains the
  model's stream reader), and emits each as an SSE event. It reuses the
  `resolveAndAssemble` helper from `agent_session.go`. No agent changes for v1.
- **`ListConversations`** added to `ConversationStore` (the only data-layer
  change) so conversations can be enumerated in the UI.

## Capabilities

### New Capabilities

- `web-ui`: an embedded, client-rendered web console served by `onclaw serve`,
  exposing a JSON API over the existing service/store layer, passphrase-gated,
  with providers/agents management, conversation browsing, and live streaming
  chat.

### Modified Capabilities

- `conversation-history`: the store gains `ListConversations` so the UI can
  enumerate conversations. Today only create/append/load-history/list-messages/
  save-summary exist.

## Impact

- **Code**: new `internal/web/` package (JSON API + static host), new
  `internal/cli/serve_cmd.go`, new `web/` React app;
  `internal/store/{store,types}.go` + `internal/store/sqlite/conversation.go` for
  `ListConversations`; `internal/config` for `web.bind` / `web.port`.
- **API**: new JSON REST surface under `/api/*`; new
  `ConversationStore.ListConversations`.
- **CLI**: new `onclaw serve [--bind] [--port] [--set-password]` command.
- **Database**: stores `web_password_hash` in the existing `preferences` KV; no
  schema migration.
- **Build**: new `make ui` (Node/Vite) step; `CGO_ENABLED=0` and ARM cross-compile
  unchanged; the device never needs Node.
- **Compatibility**: additive. No existing CLI behavior changes; keyfile-mode DEK
  handling is unchanged.