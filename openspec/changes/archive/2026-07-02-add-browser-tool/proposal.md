## Why

onclaw's agent cannot browse the web — read pages, follow flows, or drive a UI. A browser
tool must fit the 2 GB ARM constraint: Chromium headless (~hundreds of MB per tab) leaves no
room alongside the LLM client, whereas **Lightpanda** (a Zig headless browser, ~123 MB peak)
is the only engine that fits. The tool must also be **engine-replaceable** (Lightpanda today,
Chromium/rod and remote CDP next) without rewriting the tool surface — the GoClaw reference
hardcodes Chrome via go-rod and is explicitly not swappable. onclaw already has a
tools-management surface (`add-tools-management`) that this tool integrates with for
enable/disable and configuration.

## What Changes

- **Engine seam:** a new `internal/browser` subsystem with `Engine`/`Context`/`Page`
  interfaces and an engine-agnostic `Manager`. Because every candidate engine speaks CDP,
  one CDP engine implementation (driven by `go-rod`) serves them all; engines are swapped by
  pointing rod at a different CDP WebSocket URL via a `LaunchStrategy`.
- **Engines:** `lightpanda` (default; spawn `lightpanda serve`, connect rod to its CDP port),
  `chromium` (rod launcher), `remote` (resolve a wsURL via `/json/version`).
- **Driver:** `github.com/go-rod/rod` (pure-Go; preserves `CGO_ENABLED=0` and ARM
  cross-compile). rod is the CDP client; Lightpanda/Chromium/remote are the CDP servers.
- **Granular tools:** 11 builtin tools under `internal/agent/tools/browser/` —
  `browser_navigate`, `browser_snapshot`, `browser_act`, `browser_screenshot`,
  `browser_open`, `browser_close`, `browser_tabs`, `browser_status`, `browser_start`,
  `browser_stop`, `browser_console` — sharing one `Manager`.
- **Snapshot/ref contract:** `browser_snapshot` returns an accessibility tree with element
  refs; `browser_act` consumes refs; the `Page` that produced a snapshot resolves its own
  refs.
- **Integration with tools-management:** browser tools declare category `Browser`; the
  browser category registers a config schema with `ConfigRegistry`; the engine reads its
  config (engine/bin/port/headless) from `tool_group_config["browser"]`. Per-tool
  enable/disable and the category Config button are provided by `tools-management`.
- **Session persistence (v1):** in-process — a session lives for the lifetime of the engine
  process and browser context. Disk persistence (cookie save/restore) is deferred.
- **Coverage gate:** a Lightpanda CDP-coverage spike (notably `Accessibility.getFullAXTree`)
  gates whether Lightpanda is viable as the default engine.

## Capabilities

### New Capabilities

- `browser-tool`: an engine-swappable agent browser — the Engine/Context/Page seam, the CDP
  engine with Lightpanda/Chromium/remote launch strategies, go-rod as the pure-Go CDP driver,
  the 11 granular browser tools, the snapshot/ref interaction contract, configuration via
  `tool_group_config`, and the in-process session model. Depends on `tools-management`.

### Modified Capabilities

<!-- None at the spec-requirement level. Browser tools are new builtins that participate in
the existing tools-management and agent-tools assembly; they do not alter existing
requirements. -->

## Impact

- **New packages/files:** `internal/browser` (engine.go, types.go, manager.go),
  `internal/browser/cdp` (engine.go, lightpanda.go, chromium.go, remote.go);
  `internal/agent/tools/browser/` (register.go, manager.go, navigate.go, snapshot.go, act.go,
  screenshot.go, open.go, close.go, tabs.go, status.go, start.go, stop.go, console.go);
  browser unit + integration tests (integration behind `ONCLAW_BROWSER_TEST=1`).
- **Modified files:** `go.mod`/`go.sum` (add `github.com/go-rod/rod`); browser tools register
  into the existing tool registry and `ConfigRegistry` (from `add-tools-management`).
- **New dependency:** `github.com/go-rod/rod` (pure-Go, no CGO).
- **External runtime requirement:** the Lightpanda binary (default engine) must be installed
  on the host (aarch64/x86_64; no armv7 build — see risks).
- **Out of scope (deferred):** Playwright engine; disk session/cookie persistence; per-scope
  context isolation; idle-page reaper; output redaction hardening.
