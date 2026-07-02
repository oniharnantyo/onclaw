# Tasks

## 1. Dependencies & Store layer

- [x] 1.1 Add `github.com/dop251/goja` to `go.mod` (`go get github.com/dop251/goja`).
- [x] 1.2 Add `Hook` and `HookExecution` DTOs to `internal/store/types.go` (fields per design: handler_type `command|script`, config JSON shape per handler, matcher, timeout_ms, on_timeout, priority, enabled).
- [x] 1.3 Add `HookStore` (Add/Get/List/ListByScopeAndEvent/Update/Remove/Toggle) and `HookExecutionStore` (Append/List) interfaces to `internal/store/store.go`.
- [x] 1.4 Implement `sqliteHookStore` + `sqliteHookExecutionStore` in `internal/store/sqlite/hooks.go`, mirroring `sqliteSkillStore`.
- [x] 1.5 Add `agent_hooks` and `hook_executions` tables (idempotent `CREATE TABLE IF NOT EXISTS`) to `internal/store/sqlite/db.go` `Migrate()`; `hook_executions.hook_id` uses `ON DELETE SET NULL`.
- [x] 1.6 Write `internal/store/sqlite/hooks_test.go`: CRUD, toggle, `ListByScopeAndEvent`, and audit rows surviving hook deletion.

## 2. Dispatcher core (`internal/hooks`)

- [x] 2.1 Create `internal/hooks/types.go`: `Event` constants, `Payload`, `Decision`, and handler-config types.
- [x] 2.2 Define the `Handler` interface + factory registry in `internal/hooks/handler.go` (`Register`/`New`); the dispatcher resolves handlers through it with no type switch. Register `command` and `script` factories during assembly.
- [x] 2.3 Implement the regex matcher in `internal/hooks/matcher.go` (matches `tool_name`; empty pattern matches all events).
- [x] 2.4 Implement `Dispatcher` in `internal/hooks/dispatcher.go`: resolve hooks by scope(global→agent)+event+priority (cached per agent+event), match, build+cache each handler via the registry, run it, write audit row, and return `Decision`. `Fire(ctx, event, payload) (Decision, error)`.
- [x] 2.5 Implement safeguards in the dispatcher: per-hook timeout (5s default / 10s max), per-event chain budget, `on_timeout` policy, fail-closed for blocking events, and the circuit breaker (5 blocks/timeouts in 1 min → set `enabled=0`).
- [x] 2.6 Dispatcher tests: resolution order, matcher hit/miss, block short-circuit, timeout, `on_timeout` block/allow, fail-closed on handler error, circuit-breaker trip, audit row written, handler resolved via registry.

## 3. Handlers

- [x] 3.1 Implement the command handler in `internal/hooks/command.go` (registered via `commandFactory`): `exec.CommandContext` with `sh -c`, stdin = JSON payload, env allowlist (`allowed_env_vars` + safe set), `cwd`, and exit-code mapping (0=allow / 2=block+stderr / other=error).
- [x] 3.2 Implement the script handler in `internal/hooks/script.go` (registered via `scriptFactory`, which parses the goja program once at factory time): `Run` binds a fresh sandboxed runtime (only `ctx` + captured `console`), enforces the `handle(ctx)` → `{decision, reason}` contract, treats thrown exceptions as errors, and times out via `vm.Interrupt()`.
- [x] 3.3 Handler tests: command (exit mapping, env filtering, cwd, stdin payload); script (allow/block/throw, decision from return value, sandbox blocks fs/`require`, `vm.Interrupt()` timeout fires, `console` captured).

## 4. eino middleware bridge

- [x] 4.1 Read the `Wrap*ToolCall` / `InvokableToolCallEndpoint` / `ToolContext` signatures in `adk/handler.go` (`eino@v0.10.0-alpha.9`) before coding the wrap logic.
- [x] 4.2 Implement `internal/agent/middlewares/hooks_middleware.go`: `BeforeAgent`→`user_prompt_submit` (block injects denial into messages); `WrapInvokableToolCall`/`WrapStreamableToolCall`→`pre_tool_use` (block returns a wrapped endpoint yielding a denied tool result) then `post_tool_use`; `AfterAgent`→`stop`.
- [x] 4.3 Middleware tests: `pre_tool_use` block yields a denied result and the run continues; `user_prompt_submit` block injects the reason; `stop` fires on normal completion only.

## 5. Agent wiring & channel-agnostic session lifecycle

- [x] 5.1 In `AssembleAgent` (`internal/agent/agent.go`): construct the dispatcher + hooks middleware, append the middleware to the `Handlers` slice (`agent.go:188`), and store the dispatcher on `Agent`.
- [x] 5.2 Fire `session_start` automatically in `Agent.Run` via `sync.Once` (first turn); fire error-`stop` inside `eventIterator.Next()` on the terminal error event; normal `stop` remains in the `AfterAgent` middleware.
- [x] 5.3 Supply the channel at `AssembleAgent` (threaded via `agentSessionRequest.Channel`); remove the hardcoded `"cli"` default.
- [x] 5.4 Remove `StartSession`/`EndSession` calls from all channels (`cli/run.go`, `cli/chat.go`, `api/handler/chat.go`); channels now only call `Run` + iterate.
- [x] 5.5 Remove `StartSession`/`EndSession` from the `AssembledAgent` interface (`api/service/types.go`) and the `mockAgent` (`api/server_test.go`); update `AssembleAgent` callers (`agent_test.go`) for the new `channel` arg.

## 6. CLI management

- [x] 6.1 Implement `internal/cli/hooks_cmd.go`: `add/list/show/remove/toggle/test` subcommands; `add` takes `--handler {command|script}` plus handler-specific flags (`--command`/`--cwd`/`--env` vs `--script`) and `--event/--scope/--matcher/--timeout/--on-timeout/--priority`; `test` is a dry-run (no audit row).
- [x] 6.2 Register the `hooks` command in `internal/cli/app.go`; hot-reload running processes via `signalRunningProcess(st.cfg.DbPath)` after mutations.
- [x] 6.3 CLI tests for add/list/toggle/test.

## 7. REST API

- [x] 7.1 Implement `internal/api/service/hooks.go` + `internal/api/handler/hooks.go`: `GET/POST /api/hooks`, `GET/DELETE /api/hooks/{id}`, `POST /api/hooks/{id}/toggle`, `POST /api/hooks/test` (model on `handler/skill.go`).
- [x] 7.2 Register the routes in `internal/api/routes.go`.
- [x] 7.3 API tests for CRUD + toggle + test endpoints.

## 8. Web UI

- [x] 8.1 Implement `web/src/components/Hooks.tsx`: list/create/test/history panel, following `web/design-system/onclaw/MASTER.md` and the `Skills.tsx` pattern.
- [x] 8.2 Wire the panel into `web/src/App.tsx` and rebuild the bundled assets.

## 9. Verification

- [x] 9.1 `go build ./...`; `go vet` the affected packages; `gofmt -s -w .`.
- [x] 9.2 `go test ./internal/store/sqlite/... ./internal/hooks/... ./internal/agent/... ./internal/cli/... ./internal/api/...`.
- [x] 9.3 Manual end-to-end: a `command` hook that blocks `exec` via exit 2; a `script` hook that blocks destructive `rm -rf`; `onclaw hooks test` dry-run; the Web UI Hooks panel.
- [x] 9.4 Run `openspec verify --changes add-agent-hooks` (or `openspec validate --changes add-agent-hooks`) and confirm it passes.