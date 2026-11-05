# Tasks

## 1. Server package foundation

- [x] 1.1 Create the Browser Use server package (e.g. `internal/browseruse`) and relocate the browser engine into it — browser manager, profile sessions, actions, cookie jar, types — keeping rod; move the existing engine tests with it. Verify: `go build ./...` and `go test ./internal/browseruse/...` pass.
- [x] 1.2 Add the `browser-mcp` CLI subcommand (pattern: `cli/serve.go`): flags carrying the config snapshot (browser dir, default profile, headless, idle timeout, action timeout, allow-private), wires an mcp-go stdio server to the relocated engine, exits on stdin EOF, exposes no network listener. Verify: spawning the subcommand completes an MCP initialize/list-tools handshake in a scripted smoke, and the process exits when stdin closes.

## 2. Discrete tool surface

- [x] 2.1 Register the 11 discrete tools (`navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, `cookies`, `read`, `wait`) with per-tool JSON schemas and descriptions carried over from the old tool `Info()`. Verify: a server-side list-tools test asserts each tool's name and schema round-trips.
- [x] 2.2 Implement name/role targeting for `click`/`type`: case-insensitive exact accessible-name match, role-and-substring fallback, ambiguity error listing candidates, resolved against the latest snapshot. Verify: unit tests cover unique-name click, ambiguous-name error content, and fallback matching.
- [x] 2.3 Implement `read` (bounded page text with truncation indication), `wait` (network idle or fixed delay), slim interactive-action results (ok + final URL/title + changed elements only), and network-quiet settling with stable-DOM fallback for load-less pages. Verify: unit tests for read bound, wait modes, slim result shape, and settle behavior.
- [x] 2.4 Carry the guards and bounds into the server tools verbatim: private-network guard with post-redirect re-check, snapshot element/name bounds, screenshot-to-disk. Verify: the relocated guard tests pass and new tests pin read/screenshot behavior.
- [x] 2.5 Implement the single-owner lockfile in the browser dir (exclusive create, pid-staleness broken and retaken, refusal reason on live conflict). Verify: unit tests for double-run refusal and stale-lock retake.

## 3. Supervision and system server row

- [x] 3.1 Add spawn supervision to the MCP client manager: synthesize the built-in `browser-use` stdio definition (own binary + subcommand + config flags, `os.Executable`), spawn at manager init, respawn with 1s→30s backoff, stop after 5 consecutive failures surfacing failed status with the stderr excerpt, reset on successful probe. Verify: unit tests with a process seam cover respawn, cap, and reset.
- [x] 3.2 Apply configuration at spawn boundaries: settings changes persist immediately; the supervisor respawns only when no profile browser is live (janitor idle boundary), deferring otherwise, never mid-action. Verify: unit test with a fake clock covers defer-until-idle and immediate-when-idle.
- [x] 3.3 Enforce system-row semantics in the services: the system server id rejects create/update/delete, is always enabled, and is gated by the standard per-agent server allowlist with default-on posture. Verify: service tests assert mutation rejection and allowlist default behavior.

## 4. Catalog removal and allowlist migration

- [x] 4.1 Remove the `browser` built-in from the tool catalog and gate wiring (`builtin_tools.go`, `gate.go`, catalog surfaces). Verify: `go build ./...` passes and gate tests assert no `browser` catalog entry is offered.
- [x] 4.2 Add the idempotent settings-load migration mapping stored per-agent tool-allowlist entries named `browser` to the `browser-use` server key (pinned→pinned, excluded→excluded). Verify: migration tests cover the map, already-migrated no-op, and unmanged agents untouched.

## 5. Browser Use settings pane (frontend)

- [x] 5.1 Register the Browser Use pane in the settings catalog under INTEGRATIONS with icon/label/description. Verify: catalog test and SettingsView navigation test list it in the INTEGRATIONS group; both themes render.
- [x] 5.2 Build the pane body: server status line fed by probe state, engine knobs (default profile, headless, idle/action timeouts, allow-private), profile list with create/delete and default-profile protection. Verify: pane component tests cover round-trip, create/delete, and protected default.
- [x] 5.3 Build the profile import flow: source-directory picker, disclosures (close source browser, copy size, best-effort cookie decryption), validated name with collision suffix, progress and result states. Verify: pane tests mock the backend for import success, collision, and failure states.
- [x] 5.4 Remove `BrowserConfigDialog` and its ToolsPane hook. Verify: ToolsPane tests no longer reference it and the frontend suite passes.

## 6. System row and chat surfaces (frontend)

- [x] 6.1 Render the Browser Use row in the MCP pane with a System badge, no edit/remove affordances, and an open-settings action navigating to the Browser Use pane. Verify: McpPane tests cover badge, action absence, and navigation.
- [x] 6.2 Re-key tool cards and any name-keyed chat surfaces to `mcp__browser-use__*`, regenerating bindings if service APIs changed (`task common:generate:bindings`). Verify: chat tool-card tests pass against the new names.

## 7. Integration and smoke

- [x] 7.1 Go gate: `gofmt -w .`, `go vet ./...`, `go test ./...` (known flakes rerun once). Verify: all green.
- [x] 7.2 Frontend gate: full frontend test suite and production build. Verify: all green.
- [ ] 7.3 Headless live smoke in server mode: the child spawns and probes connected with tool count 11; an agent turn navigates to a public page, clicks by name, and produces a screenshot on disk. Verify: scripted smoke output captured as evidence. — DEFERRED TO USER (needs real ~/.buddies + providers); scripted parts DONE: stdio handshake smoke + isolated boot smoke (spawn, probe, lockfile, no-orphan) in .team/smoke/rebrand-browser-use/
- [ ] 7.4 Migration live check: an agent with a stored `browser` pin sees `mcp__browser-use__*` tools after load, and a user server row remains untouched. Verify: smoke transcript or DB assertion shows the mapped key. — DEFERRED TO USER; migration covered by store-level tests (browserallowlist_test.go) on synthetic DBs.
