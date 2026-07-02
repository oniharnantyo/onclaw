# Tasks

## 0. Lightpanda CDP-coverage spike (GATE)

- [x] 0.1 Install the Lightpanda nightly (aarch64) and run `lightpanda serve --port 9222`.
- [x] 0.2 Drive it with a minimal go-rod script; verify CDP coverage: `Accessibility.getFullAXTree` (snapshot — critical), `Network.setCookies`/`Storage`, `Page.navigate`, `Input.dispatch*`, `Page.captureScreenshot`, `Runtime.evaluate`, console events.
- [x] 0.3 Measure peak RSS on the target aarch64 board across ~10 heavy pages.
- [x] 0.4 Record a GO/NO-GO + coverage table + RSS in `design.md`. If AX-tree is missing, plan to default to the Chromium engine until it lands.

## 1. Engine seam & types

- [x] 1.1 `internal/browser/engine.go` — `Engine`/`Context`/`Page` interfaces (contract only).
- [x] 1.2 `internal/browser/types.go` — `Snapshot`, `ActRequest` (kinds: click/type/press/hover/wait/evaluate), `SnapshotOpts`, `ShotOpts`, `Cookie`, `ConsoleMsg`.
- [x] 1.3 `internal/browser/manager.go` — engine-agnostic `Manager` (lifecycle; reads config from `tool_group_config["browser"]` via the `Scope` handle).

## 2. CDP engine + Lightpanda launch (MVP)

- [x] 2.1 Add `github.com/go-rod/rod`; `go mod tidy`; confirm `CGO_ENABLED=0` build + `make build-all`.
- [x] 2.2 `internal/browser/cdp/engine.go` — CDP `Engine`/`Context`/`Page` impl over rod, including ref bookkeeping (snapshot emits refs; act resolves them on the same page).
- [x] 2.3 `internal/browser/cdp/lightpanda.go` — `LaunchStrategy`: spawn `lightpanda serve`, return wsURL.
- [x] 2.4 Unit tests for types/ref mapping with a fake engine; integration tests behind `ONCLAW_BROWSER_TEST=1`.

## 3. Granular tools & integration

- [x] 3.1 `internal/agent/tools/browser/manager.go` — shared `Manager` singleton (`sync.Once`).
- [x] 3.2 `internal/agent/tools/browser/register.go` — register the 11 tools (category `Browser`) and the `Browser` config schema with `ConfigRegistry`.
- [x] 3.3 Implement the 11 tool files: `navigate.go`, `snapshot.go`, `act.go`, `screenshot.go`, `open.go`, `close.go`, `tabs.go`, `status.go`, `start.go`, `stop.go`, `console.go` (each a `Tool` via `utils.InferTool`).
- [x] 3.4 Tests: tool registration; each tool dispatches to the Manager; disabled-when-engine-unconfigured returns a clear error.

## 4. More engines

- [x] 4.1 `internal/browser/cdp/chromium.go` — `LaunchStrategy` via rod `launcher` (headless flags).
- [x] 4.2 `internal/browser/cdp/remote.go` — resolve wsURL via `GET /json/version` (IPv4 host header for Chrome M113+ rebinding).
- [x] 4.3 Engine selection from `tool_group_config["browser"].engine`; tests per strategy.

## 5. Persistence, isolation, hardening

- [x] 5.1 Cookie save/restore via `KVStore` (`internal/store/sqlite/kv.go`, key `browser.session.<scope>`); plumb the store to the Manager.
- [x] 5.2 Per-scope `Context` isolation (GoClaw tenant pattern).
- [x] 5.3 Idle-page reaper (~10 min default).
- [x] 5.4 Output redaction hardening for page text/snapshots.

## 6. Verification

- [x] 6.1 `CGO_ENABLED=0 go build ./...`; `go vet`/`gofmt`; `make build-all` (amd64/arm64/armv7).
- [x] 6.2 `go test ./internal/browser/... ./internal/agent/tools/browser/...`.
- [x] 6.3 Integration (needs Lightpanda binary): an agent turn `browser_open`→`browser_snapshot`→`browser_act{click}`→`browser_screenshot`; assert refs round-trip snapshot→act.
- [x] 6.4 Tools-management integration: browser tools appear under `Browser`, toggle per-tool, category Config dialog edits engine settings.
- [x] 6.5 `openspec validate` (or `--changes add-browser-tool`) passes.
