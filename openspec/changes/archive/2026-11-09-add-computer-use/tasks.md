# Tasks

## 1. System server registry generalization

- [x] 1.1 Introduce `SystemServerSpec` registry in `internal/agents/mcp/systemserver.go` (ID, Subcommand, DisplayName, OptIn, config/status providers); convert browser-use to a spec with `OptIn=false`; replace `SystemServer()` with registry access keyed by ID. Verify: `go test ./internal/agents/...` passes unchanged; browser-use tools resolve on agents exactly as before (existing mcp tests green).
- [x] 1.2 Generalize the supervisor keys, the settings reserved-id guard (`settings_mcp.go` → reserved set), and `McpPane.tsx` (`SYSTEM_SERVER_ID` → system set) to iterate the registry. Verify: MCP pane renders the browser-use system row unchanged; settings create/update/delete/probe rejection tests pass for every registered ID.
- [x] 1.3 Add the opt-in rule to the merge point (`internal/agents/mcp.go`): opt-in specs are offered only to agents naming them, even when the allowlist is empty. Verify: new table test — empty list + opt-in spec → no tools; list naming it → tools; non-opt-in spec unchanged.

## 2. Computer-use engine package and adapter process

- [x] 2.1 Create `internal/computeruse` skeleton: `doc.go` package contract, `config.go` (Config + `DecodeStoredConfig` for the `tool_settings` "computer" doc with pointer-field semantics), fixed-refusal text vocabulary file. Verify: config round-trip test (absent vs explicit-zero fields) mirroring browseruse config tests.
- [x] 2.2 Implement `dispatch.go`: process-wide dispatch mutex with acquire/release and the fixed busy refusal. Verify: unit test — concurrent acquire gets the fixed refusal text, released lock admits the next caller.
- [x] 2.3 Implement `pin.go` + download-on-first-enable: per-release digest table, archive + binary SHA-256 verification into `~/.buddies/drivers/cua-driver/<version>/`, refuse-to-run on mismatch. Verify: test with a staged local archive (digest match succeeds; corrupted byte fails verification and no launch).
- [x] 2.4 Implement `driver.go`: spawn `serve --embedded --socket` as a direct child, private 0600 socket, socket-wait, env contract (`DO_NOT_TRACK=1`, `CUA_DRIVER_RS_TELEMETRY_ENABLED=0`, `CUA_DRIVER_RS_UPDATE_CHECK=0`, `CUA_DRIVER_EMBEDDED=1`, `CUA_DRIVER_PARENT_LIVENESS_STDIN=1`), restart-on-grant-change hook, no-orphan teardown. Verify: unit tests with a stub driver binary (socket appears; child dies with parent; env present in child process inspection).
- [x] 2.5 Implement `proxy.go`: long-lived `cua-driver mcp --socket` child via mcp-go stdio client; `server.go`: mcp-go server with the curated toolset (D4 table), `req.BindArguments` arg structs, readable tool errors; `observe.go`: PNG write to the `_meta` dir (`buddies.dev/screenshot-dir`-style key stamped by the parent with the computer subdir) + marker text emission. Verify: handler tests with a stub driver client — each tool's happy path + one error path; marker text contains the written path; fallback dir used when no `_meta`.
- [x] 2.6 Add `internal/cli/computer_mcp.go` subcommand (flags mirroring browser-mcp) and register it; wire the supervision composition `WithSystemComputerUse` / `WithoutSystemComputerUse` in `internal/cli/app.go`; server-mode compositions omit it. Verify: `go build ./...`; server-mode build (`task build:server`) exposes no computer-use spec; desktop composition spawns both system children in the app-start integration test (stubs).
- [x] 2.7 Gate darwin specifics behind build tags where cgo appears (permission probes stubbed on other GOOS). Verify: `GOOS=windows go build ./...`, `GOOS=linux go build ./...` pass.

## 3. Permission warm-up gate and settings pane

- [x] 3.1 Implement `internal/services/settings_computer.go` with `_desktop.go`/`_server.go` build-tag split: status probes, enable/disable, deep-link URLs, Check again (re-probe + daemon restart on change). Verify: service tests — ungranted state reports zero tools + awaiting status; Check-again with a flipped stub grant restarts the driver exactly once.
- [x] 3.2 Frontend `ComputerUsePane.tsx`: enable toggle, permission card (per-permission status, Manage, Check again), download state, status line; settings-nav entry; both themes. Verify: component tests render ungranted vs ready vs server-mode states; `task lint` on changed files passes.
- [x] 3.3 MCP pane second system row + `toolDisplay.ts` prefix sentences. Verify: component test renders both system rows with distinct statuses; unknown computer-use tool names fall back to the generic sentence.

## 4. Observation image lane

- [x] 4.1 Implement `internal/agents/observation_model.go`: marker scan, rolling window (N=2), path jail, user-role injection, elision rewrite, `caps.ImageInput` gate; wire beside `sanitizeModel` at the decorator build site. Verify: unit tests — injection order and count, elision of older markers, no-op on marker-free history, text-only model receives no image blocks, jail rejects escaping paths.
- [ ] 4.2 Per-family live smoke (manual, env-gated like the attachment eval): one observe→click sequence per provider family with image input on, confirming the frame reaches the model and the turn survives. Verify: recorded transcripts show injected image parts per family; smoke checklist delivered in the change notes.

## 5. Approval gate on the destructive subset

- [x] 5.1 Implement `wrapApprovalTools` in `internal/agents/attention.go` (name-suffix interception; KindApprovalRequired park; approve → proceed, deny → fixed refusal) and the adapter-side credential-field flag in tool result metadata. Verify: runner tests — gated tool parks the turn with a persisted approval event; deny feeds the fixed refusal and the turn continues; read-only tools never park.
- [x] 5.2 Frontend approval card target text for computer-use payloads (tool + target naming). Verify: component test renders the requested action and target on the pending card; card state persists post-decision like the shell approval card.

## 6. Trajectory card and bound read

- [x] 6.1 Extend `internal/services/chat_screenshots.go` with the computer screenshot bound read (same name-shape jail, `sessions/<sid>/computer/`). Verify: service tests — valid name serves bytes; path separator, `..`, and wrong-directory escapes all refuse.
- [x] 6.2 Vendor the assistant-ui computer-use element into `frontend/src/components/assistant-ui/elements/` and restyle to the desktop frame (title = app name, no URL bar); implement `ComputerTool.tsx` trajectory card (GroupedParts grouping keyed to consecutive computer-use parts, pixel→percent conversion, footer steps, type-text masking, approval-state chip); add `client.chat.computerScreenshot` transport. Verify: component tests — consecutive calls group into one card; masked type text; running step highlighted; `task lint` passes.
- [ ] 6.3 Live smoke: a multi-step desktop sequence renders one trajectory card with correct backdrop, cursor trail, and footer; transcript replay after app restart shows the same card. Verify: evidence screenshots in `.team/smoke` per repo convention.

## 7. Windows/Linux bring-up and verification sweep

- [x] 7.1 Windows: verify driver spawn (named pipe), UIPI refusal passthrough as readable errors, no permission ceremony path. Verify: cross-platform unit tests with stubs; manual smoke checklist item for a Windows machine.
- [x] 7.2 Linux: X11 default path, Wayland opt-in documented in the pane's limitation note. Verify: GOOS=linux test pass; limitation note present in ComputerUsePane.
- [ ] 7.3 Pin-bump verification procedure documented (re-pin steps, telemetry-off assertion via driver status, token-path re-verification before adopting element-token clicking). Verify: doc section in the change notes; `cua-driver telemetry status` shows disabled in the smoke output.
- [x] 7.4 Full sweep: `gofmt -w .`, `go vet ./...`, `go test ./...`, frontend `task lint` + test suite. Verify: all green; `openspec validate add-computer-use --strict` passes.

## 8. Computer-use skill seed

- [x] 8.1 Seed the `computer-use` playbook skill into the global skills tier from `internal/computeruse/skill.go` (repair-if-deleted, mirroring the knowledge/documents/memory/create-skill seeds) and call it from the app assembly. Verify: seed tests (creates when missing, idempotent, existing file stands); `go test ./internal/computeruse/... ./internal/cli/...` green; `openspec validate add-computer-use --strict` passes.
- [x] 8.2 Live-smoke hardening (notes.md live-fix log): daemon reconciliation (Manager liveness + Start replaces a dead generation + the lane's 5s reconcile loop reusing the spawnConfig gate) so a boot probe that raced a grant binding or a mid-session daemon crash self-heals; `ComputerHooks.DaemonRunning` + the dead-daemon status case (connected supervision over no daemon reads starting, never ready); the trajectory card renders the desktop frame only when the run carries an observation image. Verify: new driver/lane/status/frame tests green; full `go test ./...` 0 FAIL; frontend suite 1494/1494 (capped workers); `openspec validate --strict` valid.
