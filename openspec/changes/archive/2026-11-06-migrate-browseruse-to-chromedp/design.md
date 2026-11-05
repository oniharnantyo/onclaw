# Design

## Context

`internal/browseruse` is the sole direct go-rod consumer (ten source files, five test files). Its rod usage spans the launch pipeline (`launcher.New()` with persistent user-data dir, `Leakless(true)`, stderr-routed rod logger, `CHROME_PATH` bin override), the session manager (`profileSession{browser *rod.Browser, page *rod.Page}`, the `aliveFn` liveness probe from the dead-session relaunch fix), settle waits (`WaitRequestIdle`/`WaitStable`/sleep with context cancellation), page actions (element click/type/focus via rod `Element`, keyboard/mouse input, `NavigateBack`, full-page screenshot via `proto.PageCaptureScreenshot`, JS evaluation), cookies (export/inject jar), and the handover arcs (secret probe/fill/scrub, delivery, headed takeover relaunch). The MCP parent seam (`internal/agents/mcp`) and every tool schema live above this package and must not move. Docgen's PDF engine (picoloom) imports rod indirectly; that library is out of scope.

## Goals / Non-Goals

**Goals:**
- chromedp drives the same Chromium lifecycle (persistent per-profile user-data dir, headless default, headed takeover relaunch, `CHROME_PATH` override) with zero observable behavior change.
- Every rod convenience the package relies on is re-earned natively: leakless, settle, readable protocol errors, liveness probe.
- All stub-level seams keep their shapes — the non-Chromium test suite compiles and passes untouched.
- Existing on-disk profiles (cookie jars, Preferences, `Singleton*` handling) remain byte-compatible.
- go-rod leaves `go.mod` as a direct dependency.

**Non-Goals:**
- No new features: dialogs, network interception, extensions, downloads/HAR/PDF, and screencast all stay out (they belong to the follow-up in-app browsing change).
- No picoloom/docgen migration (rod remains an indirect dependency through it).
- No changes to tool schemas, tool names, result shapes, the `_meta` screenshot-dir channel, or anything in `internal/agents`.

## Decisions

- **D1 — Scope boundary: one package changes.** Only `internal/browseruse` (and its tests) is edited, plus one comment-level wording fix in docgen's converter (its launch-failure message names rod's cache path). The MCP tool surface, tool count, argument keys, and result shapes are byte-identical — a parent-side integration test (existing) must pass unmodified. Alternative (migrate docgen too) rejected: picoloom is a third-party library; changing it is upstream work.

- **D2 — Engine mapping.** `launcher.New()` → chromedp `ExecAllocator` pinned to the profile's user-data directory with the same flags (`--no-first-run`, `--no-default-browser-check`, headless from config, headed for takeover). `rod.Browser`+`rod.Page` per profile → one chromedp browser context with a single page target per `profileSession`, so the manager's map, janitor, and identity-checked discard/relaunch stay untouched. Rod `Element` handles (click/focus/SelectAllText/Input) → explicit CDP primitives: DOM/JS to resolve and focus, `Input.insertText` for text, and real input dispatch (`Input.dispatchMouseEvent`) wherever rod produced trusted events — the handover fill's trusted-event semantics are load-bearing. Full-page screenshot semantics preserved with chromedp's full-page capture; dimension reporting via PNG decode stays as-is.

- **D3 — Leakless replacement (highest-risk area).** Contract: no Chromium process outlives the browser-use child, graceful or crashed, by more than the watchdog interval. Graceful paths (idle reap, shutdown, discard, headed relaunch swap): chromedp context cancel plus process-group kill (`SysProcAttr.Setpgid` + kill(-pgid) on darwin/linux; Job Object on windows — derive per GOOS, never hardcode). Crash path (child killed hard): a watchdog goroutine started at each launch that polls the child's own parent liveness and force-kills the Chromium process group when the parent disappears; stale `Singleton*` cleanup already covers the relaunch side. Platform-agnostic rule per repo feedback: derive via GOOS, test the darwin/linux path in the suite.

- **D4 — Settle.** Network-idle = `Network.enable` + in-flight request counter decremented on loading finished/failed, quiet for the existing `networkIdleWindow` (1s), bounded by the action context — semantics of rod's `WaitRequestIdle`. DOM-stability fallback = JS mutation-observer poll (`chromedp.Poll` or `Runtime.evaluate` loop) matching rod's `WaitStable(500ms)` window. `runSettle`, `waitIdle`/`waitStable`/`sleepFn` seams, and their tests keep their exact shapes; only the CDP default implementations change.

- **D5 — Liveness probe.** `probeSessionAlive` becomes `Target.getTargetInfo` for the session's page target inside the existing 3s `sessionProbeTimeout` budget. Same semantics: closed tab, dead process, or wedged connection all report not-alive and the manager relaunches. The `aliveFn` seam and every acquire-path test stay untouched.

- **D6 — Error mapping.** `readableCdpError` switches to cdproto's error type, keeping the contract from the -32001 fix: protocol errors render as readable text, dead-session errors carry the "the profile relaunches on the next call" hint, and no raw struct ever reaches a tool result. chromedp's own error strings are already readable; the mapping adds the hint and normalizes wrapping.

- **D7 — On-disk compatibility.** The cookie-jar file format, `Default/Preferences` seeding (`restore_on_startup`), `removeStaleSingletonLocks`, `ProfileDir` layout, and the screenshots directory contract (`%d.png`, `_meta` routing, read service) are unchanged. An upgraded app must reuse an existing profile's login state with no migration step. Jar export/inject re-express over `Network.getCookies`/`setCookies` through the page context, in the same inject-before-first-use order.

- **D8 — Handover arc parity.** `secret.go`/`deliver.go`/`takeover.go`/`attention.go` re-express their rod operations as CDP primitives while preserving the exact orchestration: prepare/fingerprint, fill identifier before secret, SPA-keep evaluate hold vs GET-reload scrub, inspection freeze, held-takeover refusal, and the headed relaunch (allocator re-invoked with headless=false on the same directory, focused). The canary invariant (`testCanary` appears in no tool result on the receipt path) and the loopback sign-in fixtures are the regression gate; no seam signatures change.

- **D9 — Verification ladder.** (1) Stub-level suite green unchanged (manager/attention/secret orchestration). (2) Real-Chromium loopback fixtures green (navigate/settle, cookie jar round-trip, dead-browser relaunch, sign-in fill/scrub, headed takeover). (3) Engine parity smoke with the demo rig: sign-in, screenshot, and take-over scenarios re-run against the migrated child — honoring the fixture rule that any script edit requires killing and restarting the process with a confirmed startup version marker. (4) Gates: `gofmt -w .`, `go vet ./...`, full `go test ./...`; no frontend or bindings work. Known-flake rerun-once list applies.

## Risks / Trade-offs

- **Trusted-event parity**: rod's element actions may differ subtly from hand-rolled `Input.dispatchMouseEvent`/`insertText` sequences (modifier state, click counts, focus ordering). Mitigation: the loopback sign-in fixtures and the demo rig smoke exist precisely to prove the fill/submit path end-to-end; any divergence surfaces there before release.
- **Leakless edge cases**: rod's leakless is battle-tested across platforms; the replacement is new code. Mitigation: the watchdog contract is testable (kill the child in a test, assert Chromium reaps within the interval on darwin/linux); windows gets the Job Object path derived per GOOS and inherits chromedp's own cleanup as a second layer.
- **cdproto API movement**: chromedp/cdproto releases can change types between versions. Mitigation: pin the exact version pair in `go.mod`; the migration touches CDP types in exactly one package.
- **Screenshot semantics drift**: full-page capture behavior (viewport clamp vs page height) differs subtly across engines. Mitigation: the screenshot fixture asserts reported dimensions against the known fixture page.
