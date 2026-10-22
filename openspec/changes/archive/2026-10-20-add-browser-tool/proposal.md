# Proposal

## Why

Agents have no way to operate a web browser. `web_fetch`/`web_search` return text, but anything behind JS rendering, login walls, or multi-step flows (dashboards, admin panels, SaaS apps the user is signed into) is out of reach. The donor apps all solve this with a CDP-driven browser tool — upstream OpenClaw with managed profiles, the local `onclaw-v2` Go fork with go-rod (but deliberately ephemeral sessions), Hermes via a browser sidecar, KiroCrew via Playwright. The user's core requirement is the piece `onclaw-v2` dropped and OpenClaw kept: **named, persistent browser profiles whose cookies and login state survive across turns and app restarts**.

## What Changes

- **New `browser` built-in tool** (single tool, `action` sub-commands — upstream OpenClaw shape): `navigate`, `snapshot` (accessibility-tree elements with `ref` ids), `click`, `type`, `press_key`, `scroll`, `back`, `screenshot` (PNG saved under `~/.buddies/browser/screenshots/`, path returned), and `cookies` (`list`/`set`/`delete`/`clear`).
- **Named persistent profiles** under `~/.buddies/browser/profiles/<name>/` used as the Chrome `user-data-dir`: cookies, localStorage, and login state persist across tool calls, turns, sessions, and app restarts. A `default` profile is auto-created; every call may pass `profile`, else the configured default profile is used. One browser process per profile, lazily launched, reused across calls, reaped after an idle timeout.
- **Private-network guard**: navigate accepts http(s) only and refuses loopback/private-network targets unless the tool's `allowPrivate` setting (or `BUDDIES_BROWSER_ALLOW_PRIVATE`) permits them — the same posture as `web_fetch`, reusing its IP classifier.
- **Tool plumbing**: catalog entry (Tools pane card + agent wizard allowlist chips pick it up automatically), per-tool config stored under the existing `tool_settings` JSON (`defaultProfile`, `headless`, `idleTimeoutSeconds`, `actionTimeoutSeconds`, `allowPrivate`), and a settings service surface: get/save config, list/create/delete profiles.
- **Settings UI**: the Tools pane browser card gains a gear action opening a Browser config dialog (default profile picker, headless toggle, idle/action timeouts, allow-private toggle, profile list with create/delete).

**Out of scope:** attaching to the user's already-running Chrome (OpenClaw `user`/`chrome` profiles, extension relay), cloud browser backends (Hermes/Browserbase), arbitrary-JS `evaluate` action (KiroCrew withholds it deliberately), cookie import from system Chrome keychains, per-chat embedded browser panel, and rendering screenshots inline in the transcript (path return only, matching the onclaw-v2 fork's D5 precedent).

## Capabilities

### New Capabilities

(none — the browser tool joins the existing `agent-tools` capability)

### Modified Capabilities

- `agent-tools`: new `browser` tool — actions over a go-rod/CDP Chromium with named persistent profiles (cookie/login persistence across restarts), cookie management action, private-network guard, idle lifecycle, and per-tool settings + profile management surfaced in the Tools pane.

## Impact

- **Go runtime:** new `internal/agents/tools/browser.go` / `browser_manager.go` / `browser_source.go` (+ tests) — eino tool, profile-keyed rod manager, config source seam; `internal/agents/builtin_tools.go` catalog entry + constructor case.
- **Go config/wiring:** `internal/config` gains a `Browser` dir in the home layout; `internal/cli/app.go` (and desktop wiring) passes it to the agents modules and settings service.
- **Go services:** new `internal/services/settings_browser.go` (config get/save, profiles list/create/delete; delete closes a running profile browser first and refuses `default`).
- **Frontend:** `features/settings/components/BrowserConfigDialog.tsx` + `ToolsPane.tsx` gear gating; regenerated Wails bindings.
- **Dependencies:** `github.com/go-rod/rod` (pure-Go CDP; Chromium auto-discovered via `CHROME_PATH`/PATH or rod's managed download). No DB migration — config rides the opaque `tool_settings` JSON and profiles are filesystem entries.
