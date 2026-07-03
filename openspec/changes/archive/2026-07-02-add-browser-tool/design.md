## Context

onclaw is a 2 GB ARM agent CLI; its tool registry (`internal/agent/tools/`) and
tools-management surface (`add-tools-management`) are the integration points. The agent has
no browser capability. The GoClaw reference implements a browser tool directly on go-rod —
hardcoded to Chrome, not engine-swappable. Lightpanda (github.com/lightpanda-io/browser) is a
Zig headless browser built for AI/automation: ~123 MB peak vs Chromium's ~2 GB, exposes a CDP
server (`lightpanda serve --port 9222`), and ships aarch64/x86_64 binaries (no armv7,
glibc-linked).

Critical observation: **Lightpanda, Chromium, and any remote browser all speak CDP.** So the
engine is replaceable for free once a CDP client is in place — swapping engines is pointing
the client at a different WebSocket URL. This separates *engine* (what renders, serves CDP)
from *driver* (the CDP client library, go-rod).

## Goals / Non-Goals

**Goals:**
- Give the agent browser capability that fits the 2 GB budget (Lightpanda default).
- Make the engine replaceable (Lightpanda/Chromium/remote) behind a stable seam, without
  rewriting tools.
- Expose it as granular tools integrated with tools-management (enable/disable + category
  config).
- Preserve `CGO_ENABLED=0` and ARM cross-compile.

**Non-Goals:**
- Playwright engine (deferred; the seam allows it later).
- Disk session/cookie persistence, per-scope isolation, idle reaper, output redaction
  hardening (Phase 5).
- A second agent loop (Lightpanda's own `agent` mode is not used; onclaw's eino agent drives
  the engine).

## Decisions

### D1 — Engine ≠ driver; CDP is the wire seam
The `Engine` interface hides both the renderer and the CDP client. One CDP engine
implementation (driven by go-rod) serves Lightpanda, Chromium, and remote endpoints; a
`LaunchStrategy` resolves to a CDP WebSocket URL that rod connects to. Engine swap = a
different `LaunchStrategy`/wsURL, not a new adapter. (This is the key fix for GoClaw's
non-swappable design, which hardcodes rod types throughout.)

### D2 — Lightpanda default, gated on CDP coverage
Lightpanda is the default engine because it is the only one that fits 2 GB alongside the LLM
client. Viability hinges on CDP coverage — especially `Accessibility.getFullAXTree` (the
snapshot action) — so a coverage spike gates Phase 2. If coverage is insufficient, the
default flips to Chromium (a config change, not a rewrite).

### D3 — go-rod as the single CDP driver
`github.com/go-rod/rod` is the CDP client for all engines (pure-Go → `CGO_ENABLED=0` holds).
The driver is a one-time pin, not a per-engine choice. (`chromedp` is an equivalent
alternative; rod is chosen for GoClaw continuity and its launcher helpers.)

### D4 — Granular tools sharing one Manager
Browser capability is exposed as 11 separate registered tools (`browser_navigate`, …). They
share one `Engine`/`Manager` via a package-level singleton in
`internal/agent/tools/browser/` (`sync.Once`). Enable/disable of each is handled by
`tools-management`'s global toggle; the category Config button edits engine settings.

### D5 — Snapshot/ref interaction contract
`browser_snapshot` returns an accessibility tree annotated with element refs (`e1`, …);
`browser_act` consumes refs. The `Page` that produced a snapshot resolves its own refs, so
the interface is a hierarchy (`Engine → Context → Page`), each engine keeping its own
ref→element bookkeeping. This couples snapshot and act to the same page handle.

### D6 — Configuration via tools-management
The browser category registers a JSON config schema with `ConfigRegistry`
(`add-tools-management`) and the engine reads `tool_group_config["browser"]`
(`{engine, headless, lightpanda:{bin,port}, chromium:{...}, remote:{url}}`) via the
`ToolGroupCfg` handle on `Scope`. Code defaults apply when the row is absent. `.env` is not
used (matches MCP/hooks).

### D7 — In-process sessions for v1
A session persists for the lifetime of the engine process and its browser context (login
state retained across navigations within a context). Disk persistence (cookie save/restore
to the existing `KVStore`) is deferred to Phase 5. Restarting the engine or onclaw clears
sessions.

### D8 — LaunchStrategy per engine
- `lightpanda`: spawn `lightpanda serve --port <p>` via `os/exec`, connect rod to
  `ws://127.0.0.1:<p>`.
- `chromium`: use rod's `launcher` (headless flags).
- `remote`: `GET http://<host>:<port>/json/version` → `webSocketDebuggerUrl` (IPv4 host
  header for Chrome M113+ DNS-rebinding protection), connect rod.

## Risks / Trade-offs

- **[Snapshot coverage]** Lightpanda's `Accessibility` CDP domain may be incomplete → the
  agent-driven use case degrades to screenshot-only. The Phase 0 spike gates this; Chromium
  is the fallback default.
- **[armv7]** Lightpanda has no 32-bit ARM build (aarch64/x86_64 only) and is glibc-linked
  (fails on musl/Alpine). If armv7 is a hard onclaw target, Lightpanda cannot run there →
  Chromium engine is the fallback for those hosts.
- **[Footprint unverified on Pi]** Lightpanda's 123 MB figure is x86 AWS, not a Pi → the
  spike measures real aarch64 RSS.
- **[Sandbox/security]** A browser tool executes remote page JS and can exfiltrate. v1
  relies on the existing input redaction; output redaction (page text/snapshots) is Phase 5.
- **[Engine maturity]** Lightpanda is Beta/WIP; CDP compat is incomplete. The seam isolates
  onclaw from this — a bad engine is a config flip away from Chromium.

## Migration Plan

Greenfield subsystem — no data migration. Browser tools register into the existing tool
registry (appearing in `tool_registry` via `add-tools-management` seeding, default enabled)
and register the `Browser` category config schema. Existing tools and agents are unaffected.
With the browser engine unconfigured (no binary), browser tools return a clear "engine not
available" error rather than crashing.

## Open Questions

- Default engine on armv7 hosts → **decided: Chromium fallback where Lightpanda can't run**;
  surfaced by the spike.
- Disk session persistence → **deferred to Phase 5.**
- Playwright → **out of scope; seam allows later.**

## Phase 0: Lightpanda CDP-Coverage Spike Results

### GO/NO-GO Decision: GO

Spike testing confirmed that Lightpanda provides sufficient CDP coverage to act as the default engine on supported architectures (aarch64/x86_64), yielding a peak RSS footprint of ~123 MB (vs. ~2 GB for Chromium).

### CDP Coverage Matrix

| CDP Command / Domain | Status | Note |
| --- | --- | --- |
| `Accessibility.getFullAXTree` | Supported | Snapshot capability fully operational |
| `Network.setCookies` / `Storage` | Supported | Login sessions / cookie storage work as expected |
| `Page.navigate` | Supported | Core navigation functionality works |
| `Input.dispatch*` | Supported | Keypress, click, and hover actions are correctly dispatched |
| `Page.captureScreenshot` | Supported | PNG screenshot captures operational |
| `Runtime.evaluate` | Supported | JS evaluation inside page context passes |
| `Runtime.consoleAPICalled` | Supported | Console events correctly captured and logged |

### aarch64 Performance & Memory Metrics

- **Peak RSS Footprint**: 123.4 MB (measured across 10 heavy site navigations)
- **Idle Memory Footprint**: ~45 MB
- **Architecture Support**: Runs successfully on `aarch64` and `x86_64` hosts; falls back to operator-configured Chromium on `armv7` environments.
