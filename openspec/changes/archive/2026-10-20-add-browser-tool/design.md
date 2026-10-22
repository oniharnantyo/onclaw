# Design

## Context

buddies' built-in tools are eino `tool.BaseTool`s in `internal/agents/tools/` (`Info()` schema + `InvokableRun(ctx, jsonArgs)`), listed once in `internal/agents/builtin_tools.go` (`catalogEntries` + `buildDirectTool`), gated per turn by `resolveTurnTools` (catalog × global `tool_states` × agent allowlist), and configured through opaque per-tool JSON in `tool_settings` (the `web_search` precedent: `tools.WebSearchConfig` ← `storeSearchConfigSource` ← `store.ToolSettingStore`; `services/settings_websearch.go` + `WebSearchConfigDialog.tsx` for the UI). Tools receive no session/agent identity — only constructor-injected state. The home layout (`config.Layout`) already carries `Workspaces`, `Attachments`, `Skills`, `Logs`.

Reference implementations researched (2026-10-19):
- **OpenClaw upstream** — single `browser` tool with actions; `browser.profiles.<name>` = managed Playwright Chromium with its own `userDataDir` + CDP port; `defaultProfile` config; per-call `profile` param; cookies persist as plain Chrome-profile persistence; `ssrfPolicy` with private-network off by default; AX-snapshot refs.
- **onclaw-v2 fork (local, Go)** — `internal/agents/tools/browser.go` + `browser_rod.go`: go-rod over raw CDP, `browser_snapshot` from `Accessibility.getFullAXTree` filtered to interactive roles, `ref` = backend DOM node id resolved via `DOM.resolveNode`, refs invalidated on navigate, every ref action returns a fresh snapshot, screenshots to disk + path return, idle janitor. **Gap:** ephemeral `os.TempDir()` profiles — no cookie/login persistence (design D5 there).
- **Hermes** — consented snapshot of the user's real Chrome profile with cookie re-sync; ordered SSRF floors; redaction at the model boundary.
- **KiroCrew** — Playwright storage-state save/load files, attach modes, scalar-only args, `evaluate` withheld, SSRF gate on navigate.

## Goals / Non-Goals

**Goals:**
- Port the fork's proven rod action set and add the one thing it lacked: **named persistent profiles** so cookies/login state survive across calls, turns, sessions, and restarts.
- Zero schema migration; reuse the `tool_settings` JSON + settings-service + Tools-pane-dialog pattern exactly.
- Same private-network posture as `web_fetch`, same SSRF classifier.
- Browser lifecycle that cannot leak: lazy launch, per-profile reuse, idle reaping.

**Non-Goals:**
- Attaching to the user's live Chrome (extension relay / remote-debugging prompt), cloud backends, `evaluate`, system-cookie import, inline screenshot rendering, multi-tab management (one page per profile browser in v1).

## Decisions

### D1: One `browser` tool with an `action` enum, not ten `browser_*` tools
Follows upstream OpenClaw. One catalog row, one gate entry, one settings key, one allowlist chip; the model sees a compact schema. The fork's ten-tool facade needed a special alias in its gate (`deny: ["browser"]` stripping `browser_*`) — unnecessary here. Actions: `navigate`, `snapshot`, `click`, `type`, `press_key`, `scroll`, `back`, `screenshot`, `cookies`. Args are scalars only (KiroCrew precedent). **Alternative:** per-action tools (rejected — catalog/gate/UI churn ×9).

### D2: go-rod over raw CDP
Pure Go, no Node sidecar, matches the fork's lineage so `browser_rod.go` ports nearly 1:1. Chromium discovery = `CHROME_PATH` env → rod's default lookup (installed Chrome/Chromium/Edge/Brave) → rod's managed download. **Alternatives:** playwright-go (needs a Node driver install step), chromedp (viable, but the fork's snapshot/ref code is rod-shaped).

### D3: Profile = persistent `user-data-dir` at `~/.buddies/browser/profiles/<name>/`
Cookies, localStorage, IndexedDB, and login state persist by plain Chrome-profile persistence — the OpenClaw managed-profile mechanism, and the exact gap in the fork. `default` is auto-created on first use. Profile names are validated as `^[a-z0-9][a-z0-9_-]{0,31}$` (path-traversal-proof). The LLM may pass `profile` on any call; absent → `defaultProfile` from config → `"default"`. The profile registry is the filesystem (directory listing), not a DB table. **Alternatives:** Playwright-style `storageState` export/import (rejected — rod has no equivalent, and it loses non-cookie state); Hermes-style real-profile snapshot (rejected — keychain decryption, consent UX, scope).

### D4: Process-wide browser manager keyed by profile, idle-reaped
`buildDirectTool` runs per turn, so a per-tool-instance pool would relaunch Chrome every turn. The manager is a package-level singleton (`sync.Once`), map `profile → {browser, page, lastUsed}` with a per-profile mutex (actions on one profile are serialized; different profiles run concurrently). A single janitor goroutine (30 s tick) closes browsers idle longer than `idleTimeoutSeconds` (default 120). Headless flag and timeouts are read at launch — config changes apply to the next launch of that profile. Cross-session sharing of a profile browser is intentional (OpenClaw behavior). `ShutdownBrowserProfile(name)` is exported so profile deletion closes a live browser before removing its directory. **Alternative:** session-keyed sessions torn down per run (the fork — rejected, it is what prevents persistence of the live browser and forces relaunch per turn).

### D5: Snapshot refs = backend DOM node ids from the AX tree; ref actions return a fresh snapshot
Port of the fork: `Accessibility.getFullAXTree` filtered to interactive roles (button, link, textbox, checkbox, radio, combobox, menuitem, tab, option, switch, slider, searchbox, …) plus headings/text landmarks capped for size; `ref` is the backend node id, resolved with `DOM.resolveNode` at action time. Refs invalidate on `navigate`. `click`/`type`/`press_key`/`scroll`/`back` return `{ok, url, title, elements}` so the model never needs a second call to see the result. Snapshot JSON is capped (elements count + name length) to keep tool results bounded.

### D6: Private-network guard mirrors `web_fetch`
`navigate` requires `http`/`https`, resolves the host, and refuses loopback/private/link-local/unique-local addresses using the existing `isPublicIP` classifier in the same package, unless `allowPrivate` (config) or `BUDDIES_BROWSER_ALLOW_PRIVATE=1` is set. Redirect chains are re-checked post-load by validating the final `page.Info().URL`'s host. `file://` and other schemes are refused unconditionally. **Non-goal:** DNS-rebinding defense beyond the post-load check (same residual risk as `web_fetch`).

### D7: Screenshots are files, not inline images
PNG written to `~/.buddies/browser/screenshots/<unixmilli>.png` (0700 dir / 0600 file); the tool result carries `{path, width, height}`. Matches the fork's D5 ("inline image rendering is a non-goal"). Rendering screenshots in the transcript via the attachments capability store is a natural follow-up change, not part of this one.

### D8: Cookies action is explicit CDP cookie management on the profile browser
`cookies` with `cookie_op`: `list` (current page's cookies via `Network.getCookies` for the page URL, or all when no page), `set` (`name`,`value`, `domain`/`path`/`url` scoping, `secure`/`httpOnly` defaults from CDP), `delete` (by name + domain/url), `clear` (`Network.clearBrowserCookies`). Values are returned to the model on `list` — the profile is the agent's own jar, not the user's system Chrome. **Alternative:** rely only on implicit persistence (rejected — the user asked for the agent to "use cookies", which includes injecting a known cookie/session token).

**Session-cookie persistence (found by the two-process live check):** Chrome discards *session* cookies (no expiry) on browser close unless the profile's startup preference is "continue where you left off" (`session.restore_on_startup = 1`, which is what makes Chrome's `ShouldPersistSessionCookies()` true). Live testing proved the preference alone does **not** make headless Chromium persist site-set session cookies, so persistence is delivered three ways: (a) `cookies set` defaults `Expires` to now + 365 days so tool-set cookies are persistent; (b) the manager seeds `Default/Preferences` with `{"session":{"restore_on_startup":1}}` when it creates a profile directory (never overwriting an existing file — Chrome owns it afterwards), kept as defense-in-depth; (c) **the cookie jar is the load-bearing mechanism**: teardown (idle reap and explicit shutdown, under the session lock) exports every cookie via `browser.GetCookies()` to `<profileDir>/buddies-cookies.json` (0600), and the next launch re-injects it before the first action (zero expiry round-trips as a session cookie; a missing/corrupt jar is an empty cache, not an error). The live test asserts a site-set session cookie and a tool-set cookie both survive a `ShutdownBrowserProfile` → relaunch cycle.

### D9: Config lives in `tool_settings` under key `browser`; settings service mirrors `settings_websearch.go`
`tools.BrowserConfig{DefaultProfile, Headless, IdleTimeoutSeconds, ActionTimeoutSeconds, AllowPrivate}` with defaults `"default"/true/120/30/false`; `tools.BrowserConfigSource` interface + `storeBrowserConfigSource` adapter (tools stays store-free). `SettingsService` gains `BrowserConfig()`, `SaveBrowserConfig(cfg)`, `BrowserProfiles()`, `CreateBrowserProfile(name)`, `DeleteBrowserProfile(name)` — delete refuses `default`, closes a live browser for that profile, then removes the directory; if the deleted profile was `defaultProfile`, config falls back to `default`. The service receives the browser dir via its constructor (wired from `config.Layout.Browser`).

### D10: Home layout gains `Browser`
`config.Layout.Browser = ~/.buddies/browser` (with `profiles/` and `screenshots/` created lazily by the manager at 0700). `internal/config` ensure logic creates the root like the other dirs; `internal/cli/app.go` passes it via an `agents.WithBrowserDir(...)`-style option into the tool constructor and into the settings service. Tests for `Resolve`/ensure pin the new field.

### D11: No approval gate in v1
Browser actions do not interrupt for approval. The dangerous-shell interrupt machinery (`chat-approvals`) could later gate `cookies set`/navigation to non-allowlisted hosts if needed; keeping v1 approval-free avoids coupling this change to the Runner interrupt path.

## Risks / Trade-offs

- [Chromium download on first use] → rod's launcher downloads a managed Chromium (~150 MB) when none is installed; `CHROME_PATH` short-circuits it. Surface a clear error (`browser: no Chromium found; set CHROME_PATH`) when the download fails. Live tests are env-gated (`BUDDIES_BROWSER_LIVE_TEST=1`), so `go test ./...` never needs a browser.
- [Profile dir locked by a crashed Chrome] → Chrome's `SingletonLock` in the user-data-dir can block relaunch after a crash; the manager removes stale `Singleton*` files before launch when no browser is tracked for that profile.
- [Shared profile across concurrent sessions] → per-profile mutex serializes actions; two agents on one profile wait on each other (acceptable; use separate profiles for parallel work).
- [Model exfiltrates cookie values via `cookies list`] → the jar belongs to the agent's own profile (never the user's system Chrome); documented in the tool description. A later change can add a redaction boundary à la Hermes.
- [Stale Wails bindings] → the frontend task regenerates bindings (`task generate:bindings`) and runs `npm run build`; known repo gotcha.
- [Browser processes outlive the app] → idle reaper plus rod's `leakless` launcher kills children when the parent dies.

## Migration Plan

No database migration. New directory `~/.buddies/browser/` is created on demand; absence is harmless. Rollback = revert the binary; profile directories are inert data the user may delete.

## Open Questions

- Should screenshots also be stored through the attachments capability store so the chat can render them inline? Deferred to a follow-up change (needs transcript rendering work).
- Whether to expose `evaluate` behind an explicit `allowEvaluate` setting (OpenClaw does, KiroCrew does not). Deferred; not needed for the cookie/profile requirement.
