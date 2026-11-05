# Proposal

## Why

The browser-use subsystem drives Chromium through **go-rod**, which has been frozen since v0.116.2 (mid-2024): no releases, no protocol regeneration. Its bundled CDP types are 2023-era, and every new Chrome release drifts further from them — a compatibility risk that compounds for a subsystem whose entire value is reliable driving of whatever Chromium the user has installed. **chromedp** is actively maintained (v0.20.x with cdproto v0.157.x, updated through Oct 2026) and regenerates cdproto from the CDP protocol repository, so it tracks current Chrome. Migrating now, before the planned in-app browsing/takeover-live-view work builds on the engine, means that feature lands on the maintained engine instead of deepening the frozen one.

This is a **strict parity migration**: the MCP tool surface, all secret-handover invariants, and every externally observable behavior are preserved byte-identically. No capability requirement changes.

## What Changes

- `internal/browseruse` swaps go-rod for chromedp (+ cdproto) across all ten source files that import it: launch/session lifecycle, navigate/settle, page actions (click/type/keys/scroll/back/read/screenshot), snapshot, cookies, and the four handover files (secret/deliver/takeover/attention).
- rod conveniences are re-earned natively:
  - **Leakless** (rod killed Chromium when the child died) → process-group kill on graceful teardown plus a parent-liveness watchdog for crash paths, derived per GOOS.
  - **Settle** (network-idle with DOM-stability fallback) → a CDP in-flight-request counter and a DOM-stability wait, behind the existing `runSettle` seams.
  - **Readable CDP errors** (`readableCdpError` maps rod's error struct) → the same contract over cdproto's error type, keeping the relaunch hint for dead-session errors and never printing a raw struct.
  - The **liveness probe** (`aliveFn`/`probeSessionAlive`) and every other test seam (`closeFn`, `exportFn`, settle fns, handover fns) keep their shapes so stub-level tests run unchanged.
- Profile compatibility is preserved: the cookie-jar file format, seeded Preferences, stale `Singleton*` cleanup, and per-profile user-data directories are all unchanged — existing profiles and saved logins survive the upgrade untouched.
- `CHROME_PATH` override semantics are kept; chromedp's allocator launches the same persistent user-data directory per profile, headless by default with the takeover relaunch going headed.

## Capabilities

### New Capabilities

(none — this change is a pure implementation migration)

### Modified Capabilities

(none — `skip_specs: true`; the browser-use capability requirements are engine-agnostic and unchanged)

## Impact

- **Code**: `internal/browseruse` (10 source files + 5 test files with rod imports). `internal/agents`, `internal/agents/mcp`, bindings, and the frontend are untouched — the MCP seam stays byte-identical. One comment-level copy fix in `internal/agents/tools/docgen/converter.go` (its launch-failure wording references rod).
- **Dependencies**: `go.mod` gains chromedp/cdproto; go-rod is dropped as a **direct** dependency. It remains **indirect** through picoloom (docgen's PDF engine) — out of scope here.
- **Specs**: none modified (skip_specs). The rebrand spec's browser-lifecycle requirements already describe behavior, not engine.
- **Verification**: full Go suite including the real-Chromium loopback fixtures (skipped only where no browser exists), plus the demo smoke rig scenarios (sign in / screenshot / take over) re-run against the migrated child as an engine parity smoke.
