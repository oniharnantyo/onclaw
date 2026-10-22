# Tasks

## 1. Foundation & rebrand

- [x] 1.1 Rename module to `github.com/oniharnantyo/buddies` (go.mod, imports), delete `GreetService` and template demo code; verify `task build` and `task dev` still work and `go vet ./...` passes
- [x] 1.2 Add Go dependencies: `modernc.org/sqlite`, `cloudwego/eino v0.10.0-alpha.35` + `eino-ext` model components, `urfave/cli/v3`; verify `go build ./...` succeeds with the pins recorded in go.mod
- [x] 1.3 Create `internal/` package skeleton (`cli`, `config`, `store`, `store/sqlite`, `agents`, `sessions`, `gateway`, `services`) with doc.go for each; verify `go build ./...` passes

## 2. Server-mode spike (design D1/D7 validation)

- [x] 2.1 Build a throwaway spike: minimal Wails app with one bound service using events and `Stream()`, built `-tags server`, two browser tabs connected simultaneously; verify both tabs receive event broadcasts and stream deltas (record findings in the change dir, e.g. `spike-server-mode.md`)
- [x] 2.2 Verify WS reconnect behavior after server restart and record result in the spike notes; if server mode is unusable, stop and document the fallback decision (REST host over the same core) before continuing

## 3. Data store

- [x] 3.1 Implement `~/.buddies` path resolution and idempotent first-run layout (db, skills/, logs/); verify with a unit test using an isolated HOME that all paths are created
- [x] 3.2 Implement embedded forward-only schema migrations with a version table, run-before-services gate, and fail-startup-on-error; verify a unit test migrates a v0 database and a deliberately failing migration prevents startup
- [x] 3.3 Define store sub-interfaces (agents, providers, mcp servers, hooks, gateway settings, session events, checkpoints, skill registry) modeled on onclaw-v2's store pattern plus in-memory test fakes; verify fakes compile and pass `go test ./internal/store/...`
- [x] 3.4 Implement the `store/sqlite` adapter (WAL, busy timeout, atomic config writes, session event append + replay, checkpoint storage); verify unit tests cover atomicity (failed write leaves no partial config) and event replay reconstructs a transcript
- [x] 3.5 Verify concurrent-write safety: unit test writing session events and a config change simultaneously completes both (go test -race)

## 4. Agent runtime port

- [x] 4.1 Port onclaw-v2's agent manager core de-tenanted onto eino v0.10.0-alpha.35 (turn execution, session context assembly); verify unit tests with a fake model component produce deterministic multi-turn behavior
- [x] 4.2 Implement provider registry with Anthropic (`eino-ext` claude) wired end-to-end and additional adapters registering but not required; verify registry unit test lists models and rejects unconfigured providers with a clear error
- [x] 4.3 Implement token streaming with incremental event emission (text deltas, reasoning, tool activity, completion, error) persisted per turn regardless of client disconnect; verify unit test replays a completed turn from store after a simulated disconnect
- [x] 4.4 Implement context-window accounting (tokens used vs model limit exposed to callers); verify unit test asserts accounting grows across turns and matches the model's declared limit
- [x] 4.5 Implement compaction at threshold: summarize to checkpoint, persist, continue within limit, emit compaction marker event; verify unit test drives a session past the threshold with a fake model and asserts the next turn succeeds with a compacted context
- [x] 4.6 Document the runtime package boundary (eino types stay inside) in `internal/agents/doc.go`; verify `go doc` renders and `go build ./...` passes

## 5. Services & bindings

- [x] 5.1 Implement `internal/services` thin wrappers (agents, sessions/chat, settings, app info) over the core with no business logic; verify unit tests call through wrappers against fakes
- [x] 5.2 Register services in `internal/cli` composition root used by both mains; verify a unit test builds the full service set from a temp data dir
- [x] 5.3 Generate TypeScript bindings (`wails3 generate`) and commit them; verify frontend `npm run build` compiles against the generated modules

## 6. Desktop app shell

- [x] 6.1 Rework `main.go` (`//go:build !server`): build services via `internal/cli`, create window, set `ApplicationShouldTerminateAfterLastWindowClosed: false`; verify `task run` opens the app and closing the window leaves the process running
- [ ] 6.2 Add system tray with Open / gateway state + pause-resume / Quit (quit is the only full exit); verify manually via `task run`: close window → tray present → Open restores window → Quit exits cleanly
- [x] 6.3 Implement gateway seam skeleton in `internal/gateway` (running state, pause/resume, no trigger adapters yet) reflected in tray state; verify pause/resume toggles tray state and a unit test covers state transitions

## 7. Server mode

- [x] 7.1 Implement `main_server.go` (`//go:build server`): same services, `Options.Server` bound to loopback by default; verify `task build:server` produces a binary that serves the frontend in a browser with no GUI libs linked (check `otool`/`ldd` on the binary)
- [x] 7.2 Add optional bearer token gate for HTTP and WebSocket connections; verify a request/connection without the token is rejected when configured and accepted when not
- [ ] 7.3 Wire `Taskfile.yml` `build:server`/`run:server` to the real binary and verify the Docker build task produces a runnable distroless image (`task build:docker` + container smoke test)
- [ ] 7.4 Verify cross-host parity against the store: create an agent and chat on desktop, then open server mode against the same `~/.buddies` and confirm agent and transcript match

## 8. Frontend foundation

- [x] 8.1 Add frontend deps (daisyUI with light+dark themes, `@assistant-ui/react` + markdown/generative-ui, `react-nice-avatar`, zustand) and wire daisyUI theming; verify `npm run build` passes and a placeholder page renders in both themes
- [x] 8.2 Build the three-menu shell (Chat / Agents / Settings navigation) with theme toggle; verify navigation works in `task dev` and both themes render legibly
- [x] 8.3 Implement the bindings client wrapper (typed calls, event subscriptions, stream helper, connection status for server mode); verify a unit test against generated modules and a dev-mode roundtrip call to the app info service

## 9. Chat menu

- [x] 9.1 Port session list per agent (latest activity, new session, switch) onto bindings; verify switching sessions renders the persisted transcript after app restart
- [x] 9.2 Port transcript rendering (user/agent messages, tool-call cards, reasoning bubbles, compaction markers); verify component tests for each entry type and a manual streaming turn renders incrementally — deviation: rendered with react-markdown directly; assistant-ui was dropped because the donor's assistant-ui machinery was shaped around tenancy affordances the base app does not have (see design.md D6 note + verification-notes.md)
- [x] 9.3 Build the right sidebar (agent avatar, model, context-ring usage) and verify collapsing it does not interrupt an in-flight turn
- [x] 9.4 Port composer affordances (Enter to send, slash menu, skill picker, mention menu, queued messages while busy); verify component tests for menus and queue ordering, plus a manual queued-message turn
- [x] 9.5 Verify chat menu in both daisyUI themes (transcript, sidebar, composer menus, tool cards) and fix any contrast/layout issues

## 10. Agents menu

- [x] 10.1 Port agents card grid with react-nice-avatar avatars (name, model, state; chat + edit affordances); verify card reflects a model change and chat affordance opens the agent's latest session
- [x] 10.2 Implement agent creation (minimal name) with deterministic avatar; verify created agent persists across restart and appears in the grid
- [x] 10.3 Port agent configuration sections (provider/model, system prompt, skills, tools, MCP opt-ins, hooks) with atomic save; verify a model change applies to the next turn without restart
- [x] 10.4 Implement agent deletion with confirmation that preserves session history; verify deleted agent disappears while its sessions remain in the store
- [x] 10.5 Verify agents menu in both daisyUI themes

## 11. Settings menu

- [x] 11.1 Port providers pane (add/edit/remove, credential storage, model listing); verify an added provider's models appear in agent configuration
- [x] 11.2 Port skills pane (directory-backed listing, global enable/disable); verify a disabled skill disappears from agent config and composer skill menu
- [x] 11.3 Port tools pane (listing, toggles) and verify a disabled tool is absent from the next agent turn
- [x] 11.4 Port hooks pane (view/add/edit/remove, test affordance) and verify the test action reports success/failure
- [x] 11.5 Port MCP pane (add/edit/remove server definitions, connection state) and verify an added server is listed and opt-in-able from agent config
- [x] 11.6 Verify settings menu in both daisyUI themes

## 12. Integration & final verification

- [x] 12.1 Full verification pass: `gofmt -w .`, `go vet ./...`, `go test ./...`, frontend `npm run build`, `task build` (desktop) and `task build:server` all green
- [ ] 12.2 End-to-end walkthrough per spec scenarios: tray lifecycle, chat with streaming + compaction marker, agent CRUD, settings panes, theme parity — record results in the change dir
- [x] 12.3 Update README (buddies overview, ~/.buddies layout, desktop + server run instructions incl. token flag); verify documented commands run as written
