# add-computer-use — verification notes (task groups 4 and 7)

Operational notes for the Computer Use lane's cross-platform bring-up, the
vendored driver's pin lifecycle, and the observation lane's per-family live
smoke. Task references: 4.2 (per-family image-lane smoke), 7.1 (Windows
smoke checklist), 7.2 (Linux note), 7.3 (pin-bump procedure).

## 4.2 Observation image lane — per-family live smoke (env-gated, manual)

The `observationModel` decorator (design D5) injects the two most recent
observation screenshots as one trailing user message with image blocks. All
three connector families support a user message between tool result and the
next assistant step structurally — the smoke proves each family actually
tolerates it on a live turn. Run once per family (Claude, openai, Gemini)
on a vision-capable model with image input enabled:

1. **Sequence.** In a fresh session on an agent whose allowlist names
   `computer-use`, drive one observe→act round trip through the desktop
   (e.g. `observe_desktop`, then `click` by coordinates). Three consecutive
   observation markers exercises the window: the third `observe_*` result
   must elide the first marker to `(earlier screenshot elided)` in the
   outbound view while injecting the two most recent frames.
2. **Injected-parts assertion.** With request logging on for the family,
   confirm the outbound request carries the synthesized user message after
   the tool results: text part `[computer-use observation]` + one image
   block per injected marker, oldest→newest — and that the persisted
   transcript keeps marker TEXT only (no base64 in session events).
3. **Turn survival.** The turn completes normally after the injection — no
   connector 4xx on the interleaved user message, no empty response, resume
   after an app restart replays the same elided view.
4. **Text-only control.** Repeat one sequence on a text-only model of the
   same family: no image blocks in the outbound request, markers verbatim,
   the accessibility tree remains the observation channel.
5. **Elision leak check.** The elided marker text is the only trace of
   older frames; confirm no earlier image data reaches the connector.

Failures here degrade per design: the decorator is one flag to disable per
family if a family chokes on the interleaving (design D5 risk note).

## 7.3 Pin-bump procedure (tasks 2.3 / 7.3)

The driver is digest-pinned per release in `internal/computeruse/pin.go`
(design D1, spec computer-use "Driver acquisition and integrity"). The
current pin is **cua-driver-rs v0.41.2** (tag `cua-driver-rs-v0.41.2`,
repo [trycua/cua](https://github.com/trycua/cua/releases)); the archive
digests were cross-verified against the release's `checksums.txt` and the
GitHub API's per-asset `digest` fields, and the binary digests were computed
from the extracted archives. A bump re-walks the whole chain:

1. **Pick the release.** Resolve the newest `cua-driver-rs-v*` tag in
   trycua/cua. GitHub may label driver releases "Pre-release" purely to keep
   the monorepo-wide "Latest" pointer on one product — the release body says
   so explicitly when that is the case; a plain Cua Driver SemVer release is
   stable for pinning. Never pin a `nightly-*` tag.
2. **Verify the archive digests.** Download each platform archive
   (`darwin-universal-binary.tar.gz`, `windows-{x86_64,arm64}-binary.zip`,
   `linux-{x86_64,arm64}-binary.tar.gz`) plus `checksums.txt` from the tag.
   Check every downloaded archive against `checksums.txt` **and** the
   GitHub API asset `digest` fields. Two independent sources must agree
   before a digest enters the pin table.
3. **Verify the launch binary digests.** Extract each archive in a throwaway
   directory and compute SHA-256 over the launch binary (`cua-driver`,
   `cua-driver.exe`). Update `pinnedVersion`, `pinnedTag`, and the
   `pinnedAssets` table (archive name, archive SHA-256, format, binary name,
   binary SHA-256). The vendor's code signature is never touched — extract
   and chmod only; never re-sign.
4. **Re-verify the tool-name mapping.** Run `cua-driver mcp --socket <sock>`
   against a booted daemon and list its tools. Diff the adapter's
   `driverToolName` map (`internal/computeruse/server.go`) against the real
   surface — every curated tool's backing name, plus the screenshot
   convention (image/png content block) and the `_meta` "elements" shape
   (`token`, `secure`) the C2 secure-field annotation reads. The map is the
   adapter's entire coupling to the driver; a drift here is a silent break.
5. **Telemetry-off assertion.** Boot the daemon from the new pin with the
   spawn env contract
   (`DO_NOT_TRACK=1`, `CUA_DRIVER_RS_TELEMETRY_ENABLED=0`,
   `CUA_DRIVER_RS_UPDATE_CHECK=0`, `CUA_DRIVER_EMBEDDED=1`,
   `CUA_DRIVER_PARENT_LIVENESS_STDIN=1`) and run
   `cua-driver telemetry status`. It must report telemetry **disabled** and
   no pending update check. The env names carry the `_RS_` infix and have
   drifted before (design D1); if the status output shows telemetry active,
   stop — the env contract in `internal/computeruse/driver.go` needs the
   corrected names before the pin can ship. Watch egress during the smoke:
   no PostHog traffic is the pass condition the design risk note names.
6. **Token-path re-verification before adopting element-token clicking.**
   The v1 posture leans coordinate+capture and AX `set_value` because of
   upstream token-path regressions (#5910). On the new pin, click by element
   token across: a plain button, a button after a window resize, and a
   control in a different app than the observed one. Only if all three
   resolve correctly (no neighbor clicks, no silent no-ops) may the tool
   descriptions' guidance shift toward element tokens; otherwise keep the
   coordinate-first guidance and the fail-closed stale-token behavior.
7. **Record the pin.** Append the verified tag + digests to this note's pin
   history and run the full verification sweep (task 7.4).

### Pin history

| Version | Tag | Verified | Notes |
|---|---|---|---|
| 0.41.2 | `cua-driver-rs-v0.41.2` | 2026-11-07 | Initial pin. Archives cross-verified vs `checksums.txt` + GitHub API digests; binaries computed from extracted archives. |

## 7.1 Windows smoke checklist (tasks 2.7 / 7.1)

Manual smoke on a real Windows machine before calling the platform done.
The adapter/engine code is cgo-free and cross-builds (`GOOS=windows go
build ./...`); everything below is runtime behavior:

- [ ] **Named pipe transport.** The daemon's private local endpoint is a
      named pipe on Windows (`--socket <\\.\pipe\...>` path from the
      manager). Confirm the daemon boots from the pinned install under
      `%USERPROFILE%\.buddies\drivers\cua-driver\<version>\`, the pipe
      appears, and the adapter's `cua-driver mcp --socket <pipe>` frontend
      completes the MCP handshake. The pipe must be private to the user
      session (the 0600 chmod is a no-op on pipes — verify the driver's
      default ACL instead).
- [ ] **No-orphan teardown.** Kill the buddies app: the daemon (and the
      adapter) must be gone within the watchdog interval. Check Task Manager
      for `cua-driver.exe` processes. The kill-on-close Job Object
      (`processkill_windows.go`) plus the daemon's stdin liveness
      (`CUA_DRIVER_PARENT_LIVENESS_STDIN=1`) are the two layers.
- [ ] **UIPI refusal passthrough.** Launch an elevated target (Task Manager,
      an admin command prompt) and attempt a click/type against it from a
      non-elevated buddies. The driver reports the UIPI refusal — the smoke
      asserts it surfaces as a **readable tool error** in the turn (the
      fixed `ActionFailureGuidance` framing), never as a turn failure, a
      crash, or a silent no-op. The model should be able to read the refusal
      and pick another target.
- [ ] **No permission ceremony.** Windows has no Accessibility/Screen
      Recording equivalent prompt: enabling Computer Use must go straight to
      ready once the driver is downloaded and verified — no permission card
      state, no system dialog, no waiting-for-grant hold. The settings
      service's `_desktop.go` seam reports "no permission ceremony" on
      windows (design D6/D9).
- [ ] **Digest refuse-to-run.** Corrupt the installed `cua-driver.exe`,
      re-enable: the pane reports the integrity failure, nothing launches,
      no computer-use tools are offered (spec "Digest mismatch refuses to
      run").
- [ ] **Env contract.** While the daemon runs, dump its environment (Process
      Explorer properties): exactly `DO_NOT_TRACK=1`,
      `CUA_DRIVER_RS_TELEMETRY_ENABLED=0`, `CUA_DRIVER_RS_UPDATE_CHECK=0`,
      `CUA_DRIVER_EMBEDDED=1`, `CUA_DRIVER_PARENT_LIVENESS_STDIN=1` — no
      parent variable leakage.

## 7.2 Linux note (tasks 2.7 / 7.2)

- **X11 is the default and the supported path.** The driver's X11 backend
  needs no setup beyond a running session; the smoke is the same shape as
  macOS (spawn, socket, one observe→click round trip).
- **Wayland is opt-in and limited.** The driver ships a GNOME Shell helper
  (`wayland-helper/winrects@cua` inside the linux archives) and the driver
  must be run with its Wayland backend enabled explicitly. Under Wayland,
  global capture and input injection are restricted by the compositor:
  expect partial window-state metadata and no cross-workspace capture. This
  limitation is surfaced in the settings pane's limitation note (task 7.2);
  treat Wayland as best-effort, X11 as the contract.
- **Hyprland** ships a separate plugin asset in the release; out of scope
  for v1.
- **No permission ceremony**, like Windows: the Linux path has no TCC
  equivalent; enabling goes straight to ready after the pin verifies.
- Cross-platform build/test gate for the package stays cgo-free:
  `GOOS=linux go build ./...` and the package's unit tests must pass on
  every task-7.4 sweep.

## Live-fix log 2026-11-07 night (post-implement smokes)

- **FIX 5 — daemon-less lane (user report: every call "transport handshake
  error").** After a Quit & Reopen grant flow, the adapter spawned but no
  daemon existed: boot warm-up probed permissions ONCE, raced the launch
  (grants bind mid-launch), silently returned, and nothing ever spawned the
  daemon — while the supervisor's per-tick `spawnConfig` re-probe correctly
  spawned the adapter. Nothing healed a later daemon crash either, and the
  pane read "ready" over the dead daemon (lying-status family). Fix, three
  parts: (1) `computeruse.Manager.Running` reads real process liveness and
  `Start` replaces an exited generation; (2) the lane's warm-up became a
  reconciliation loop (`computerWarmTick` 5s) whose tick brings the daemon up
  exactly when the warm-up gate — `spawnConfig`, the single gate definition —
  answers ready and none runs, cancelled + flagged at close so no respawn
  races teardown; (3) `ComputerHooks.DaemonRunning` + a status case: adapter
  connected over a dead daemon reads `starting`, never `ready`. Tests:
  dead-generation replacement (stub exit via stdin EOF), reconcile holds a
  closed gate, status table cases. Spec: "Driver daemon lifecycle" grew the
  reconcile requirement + self-heal scenario.
- **FIX 6 — the desktop frame IS the observation (user feedback: show the
  Desktop only if an image exists).** `TrajectoryCard` renders the vendored
  chrome + backdrop only when the run carries a capture; a failed or
  not-yet-run run shows the step footer alone (previously one empty
  chrome frame per call). Spec: "Computer-use trajectory card" grew the
  frame-only-with-image requirement + no-observation scenario; tests pin
  the absent frame for observation-free runs.
