# Verification notes — add-base-gateway-app

Recorded 2026-10-15 at the end of the team implementation run. Companion to
`tasks.md` (checkbox state is authoritative) and the team verifier report
(summarized below).

## Machine-verified (team verifier, fresh evidence — all PASS)

- `gofmt -l .` clean; `go vet ./...` and `go vet -tags server ./...` exit 0.
- `go test -race -count=1 ./...` — all 9 internal packages ok.
- Frontend: vitest 13 files / 79 tests pass; `npm run build` (tsc + vite) green.
- `task build` → `bin/buddies`; `task build:server` → `bin/buddies-server`.
- Architecture boundary (design D1): zero wails imports under the core
  (`internal/{agents,sessions,store,config,gateway,services,cli}`); no eino
  types in exported signatures (`go doc` inspected).
- Generated bindings are fresh (regenerating produces byte-identical output).
- Spec scenarios verified by targeted `-race` test runs: migration gate
  prevents startup, WithTx rollback leaves no partial config, event replay
  reconstructs transcripts, concurrent session-event + config writes, disconnect
  mid-turn still persists the full turn, unconfigured provider → typed error +
  turn_error event, context usage grows vs catalog limit, compaction at
  threshold keeps transcript intact, chat Send returns turn id promptly,
  session event forwarding on `session:<id>:event`, TestHook success/failure.
- Bearer gate (httptest + live binary): with token configured, HTTP and both WS
  endpoints (`/wails/events`, `/wails/stream/ws`) reject without
  `Authorization: Bearer` (or `?token=`), accept with it; without a token
  everything passes.

## Live browser walkthrough (server mode, headless binary on 127.0.0.1)

Ran `bin/buddies-server` against a fresh temp `HOME` and drove the real UI in a
browser over the Wails WebSocket bridge:

- App shell, three-menu nav, and theme toggle render; the chat empty state
  appears only after a live `Agents.List` bindings roundtrip.
- Created agent "Scout" through the Agents menu UI (create modal → card grid
  with deterministic avatar, Open chat / Configure / Delete affordances).
- "Open chat" navigates to the chat menu with Scout selected: session rail,
  composer (Send disabled until a session exists), right sidebar with agent
  info and Context section.
- Settings: all five panes render (Providers, Skills, Tools, Hooks, MCP); the
  Tools pane lists the built-in catalog (`current_time`, `http_get`) from the
  backend; Skills reports the empty skills directory correctly.
- Theme toggle switches `data-theme`; both daisyUI themes render legibly
  (screenshots captured during the walkthrough).
- First-run layout confirmed at runtime: fresh HOME produced
  `~/.buddies/{buddies.db,logs/,skills/}` with 0700/0600 permissions.

## Not exercised (needs a human / GUI / real credentials)

- Desktop tray lifecycle (close-to-tray, Open, pause/resume, Quit) —
  `task run` walkthrough (tasks.md 6.2 left unticked for this).
- Liquid Glass window (2026-10-15): darwin builds pass `-tags private_mac_apis`
  (required for webview transparency; without it the glass stays hidden),
  `main.go` sets `MacBackdropLiquidGlass` (window-level NSGlassEffectView,
  macOS 26+), and the page layers theme-aware WebKit `-apple-visual-effect`
  materials over it with tint / backdrop-filter fallbacks (styles.css
  `html[data-glass]`). CSS-side verified in a browser with a simulated
  backdrop (both themes + modal); the real materials need a `task build` +
  GUI launch on macOS 26+.
- A real chat turn with streaming (requires a configured provider API key).
- Cross-host parity check (7.4): desktop + server against the same data dir.
- Docker image smoke (7.3 unticked; no docker daemon on this machine).
- `task build:docker` / distroless runtime.

## Known deviations / notes

- Chat transcript renders via react-markdown directly; `@assistant-ui/react`
  is declared but unused (donor assistant-ui machinery was tenancy-shaped).
  `openspec/config.yaml` lists assistant-ui as the chosen stack — accepting
  the deviation (or re-introducing assistant-ui) is a user decision.
- Wails beta.29 macOS gap: the server binary links Cocoa/WebKit but runs fully
  headless; Linux builds are GUI-free (`GOOS=linux` dependency check clean).
- `devEngines.packageManager` in frontend/package.json widened from an exact
  `11.19.0` pin to `^11.19.0` (exact pin made stock npm 11.19.1 refuse to run
  any script).
- Git index: the pre-existing staged snapshot predates the entire
  implementation — run `git add -A` before the initial commit so staged
  bindings/code match the working tree.
