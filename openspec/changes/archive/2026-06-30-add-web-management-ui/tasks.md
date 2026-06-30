## 0. Scaffold + spec

- [x] 0.1 Create this OpenSpec change (`openspec/changes/add-web-management-ui/`)
- [x] 0.2 `internal/web/` package skeleton: `server.go`, `routes.go`,
      `middleware.go`, `respond.go`, `embed.go` (`//go:embed assets/*`)
- [x] 0.3 Committed fallback `internal/web/assets/index.html` + `.gitkeep` so the
      Go build compiles before the UI is built
- [x] 0.4 `internal/cli/serve_cmd.go`: `onclaw serve` mirrors `run.go`'s recipe —
      `getProviderManager`, write PID, `StartDBWatcher` + SIGHUP handler,
      `resolveAndAssemble` for sessions — and prints `listening on <bind>:<port>`
- [x] 0.5 Wire `serve` into the command tree; add `web.bind` / `web.port` defaults
      (`0.0.0.0` / `8484`) to `internal/config`
- [x] 0.6 `GET /api/health` returns 200; SPA-fallback static handler
- [x] 0.7 `make build && ./bin/onclaw serve` boots and serves the shell;
      `make test`, `make vet` pass

## 1. Auth + Providers & secrets

- [x] 1.1 `internal/web/auth.go`: bcrypt verify against `web_password_hash` in
      `preferences`; in-memory session store; signed `HttpOnly SameSite=Strict`
      cookie; `Origin`/`Referer` CSRF check
- [x] 1.2 `onclaw serve --set-password` (interactive) writes the bcrypt hash;
      `serve` refuses to start with no hash
- [x] 1.3 Session middleware gates all `/api/*` except `/api/login`
- [x] 1.4 `handlers_provider.go`: JSON list/add/show/update/delete, set-default,
      delegating to `llm.Service`
- [x] 1.5 Masked secret status (`GET /api/providers/:name/secret` ->
      `{"set":bool,"hint":...}`) and `POST .../secret` -> `SetSecret`; never
      return plaintext
- [x] 1.6 React: login screen, providers table (TanStack) + add/edit dialog +
      set-key flow; skeleton/empty/error states

## 2. Agents CRUD

- [x] 2.1 `handlers_agent.go`: JSON list/add/show/update/delete, set-default,
      delegating to `llm.Service`
- [x] 2.2 React: agents table + full edit page (model picker, tools, workspace,
      system prompt)

## 3. Conversation viewer

- [x] 3.1 `ConversationRow` DTO in `internal/store/types.go`
- [x] 3.2 `ListConversations(ctx) ([]*ConversationRow, error)` in the
      `ConversationStore` interface (`internal/store/store.go`)
- [x] 3.3 SQLite impl + test in `internal/store/sqlite/conversation(.go|_test.go)`
- [x] 3.4 `handlers_conversation.go`: `GET /api/conversations`,
      `GET /api/conversations/:id/messages`
- [x] 3.5 React: conversations table + message view via shared `MessageBubble`

## 4. Live chat (SSE)

- [x] 4.1 `internal/web/sse.go`: SSE helper that writes and flushes one event at a
      time and honors `r.Context()`
- [x] 4.2 `handlers_chat.go`: `POST /api/chat` reuses `resolveAndAssemble`, calls
      `assembledAgent.Run(ctx, prompt)`, loops the `EventIterator` and emits each
      `*schema.AgenticMessage` as an SSE event; ends with `done` / `error`
- [x] 4.3 React: chat screen with fetch ReadableStream SSE reader + streaming
      cursor; `MessageBubble` reused from M3
- [x] 4.4 Verify messages auto-persist via the history middleware and appear in the
      M3 viewer after reload

## 5. Build, embed, verify

- [x] 5.1 `web/` Vite + TS + Tailwind v4 + shadcn init; self-hosted Geist; cold
      devtool token theme; `vite.config.ts` `outDir = ../internal/web/assets`
- [x] 5.2 `make ui` (`npm ci && npm run build`); `make build` embeds the bundle
- [x] 5.3 `make ui && make build-all` confirms linux amd64/arm64/armv7 still
      cross-compile (`CGO_ENABLED=0`); binary size sanity check
- [x] 5.4 End-to-end manual pass: login, add provider+key, create agent, view a
      prior conversation, run a streaming chat, reload to confirm persistence
- [x] 5.5 `make test` (>=80% on new Go code), `make vet`, `make lint`, `make fmt`;
      `cd web && npm run build` green