# Tasks

## 1. Engine foundation (launch + manager)

- [x] 1.1 Pin chromedp + cdproto in `go.mod`; remove go-rod as a direct dependency (it may remain indirect via picoloom)
- [x] 1.2 Rewrite `launch.go` on the chromedp `ExecAllocator`: persistent per-profile user-data dir, `--no-first-run`/`--no-default-browser-check`, headless from config, `CHROME_PATH` override, stderr-routed logging, single `about:blank` page target; inject cookie jar before first use (D2, D7)
- [x] 1.3 Implement the leakless replacement (D3): process-group kill on graceful teardown paths, parent-liveness watchdog for crash paths, derived per GOOS (Setpgid darwin/linux, Job Object windows); unit-test the watchdog (child killed → Chromium reaped) on darwin/linux
- [x] 1.4 Re-point `profileSession` fields to the chromedp context types keeping every seam (`closeFn`, `exportFn`, `aliveFn`, settle fns, handover fns) shape-identical; `probeSessionAlive` → `Target.getTargetInfo` inside the 3s budget (D5)
- [x] 1.5 Verify `manager.go` acquire/discard/relaunch, janitor sweep, and shutdown paths compile and pass their stub tests unchanged

## 2. Page primitives (navigate, settle, actions, snapshot)

- [x] 2.1 Implement settle (D4): network in-flight counter over `Network` events with the 1s quiet window, DOM-stability fallback; keep `runSettle` and its tests unchanged
- [x] 2.2 Rewrite `navigate.go` (navigate + post-load policy + info) and `actions.go` (click/type/pressKey/scroll/back/readPage/screenshot) over CDP primitives, routing user-intent clicks through trusted `Input.dispatchMouseEvent` (D2); full-page screenshot keeps reported-dimension semantics
- [x] 2.3 Rewrite `snapshot.go` element scan/ref resolution over JS evaluation with chromedp `Runtime` calls; snapshot element shape and ref semantics byte-identical
- [x] 2.4 Update the navigate/settle/relaunch tests to the new engine types, including the loopback dead-browser relaunch fixture

## 3. Cookies and profiles

- [x] 3.1 Rewrite `cookies.go` export/inject over `Network.getCookies`/`setCookies` in the preserved inject-before-first-use order (D7)
- [x] 3.2 Compatibility fixture: load a pre-migration jar file + seeded `Preferences` and assert the session restores (D7); keep `removeStaleSingletonLocks` behavior

## 4. Handover arcs (secret, deliver, takeover, attention)

- [x] 4.1 Re-express `secret.go` prepare/fingerprint/fill/scrub over CDP primitives with the exact ordering: identifier before secret, fingerprint re-check per field, SPA-keep evaluate hold vs GET-reload, scrub on submit (D8)
- [x] 4.2 Update `deliver.go`, `takeover.go` (headed relaunch on the same directory + focus), and `attention.go` freeze/refusal paths to the new engine types; no seam signature changes (D8)
- [x] 4.3 Green the real-Chromium loopback sign-in suite, including the `testCanary` invariant (canary in no tool result on the receipt path) and the SPA/GET scrub fixtures

## 5. Server surface and errors

- [x] 5.1 Switch `readableCdpError` to cdproto's error type keeping the readable-text contract and the dead-session relaunch hint (D6); update its tests
- [x] 5.2 Verify the `_meta` screenshot-dir routing and `screenshotDir` fallback are engine-agnostic and untouched; keep the screenshot tool description text
- [x] 5.3 Fix the docgen converter's launch-failure comment wording (rod cache-path reference) to the chromedp remedy, same `CHROME_PATH` guidance

## 6. Engine parity smoke + gates

- [x] 6.1 Re-run the demo smoke rig scenarios against the migrated child: sign in (card → submit → fill), screenshot (capture → `read_file` from the session workspace), take over (headed relaunch → resolve) — honoring the fixture restart rule with a startup version marker
- [x] 6.2 Confirm parent-side integration (`internal/agents` MCP resolve/wrap tests, browser-screenshot service tests) passes with zero edits
- [x] 6.3 Full gates: `gofmt -w .`, `go vet ./...`, `go test ./...` (rerun-once list applies); confirm no frontend or bindings diff
