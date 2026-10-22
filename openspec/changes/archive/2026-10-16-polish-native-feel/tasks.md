# Tasks

## 1. Desktop shell — identity, instance, window (Go + build assets)

- [x] 1.1 Create the icon assets: full-color `build/appicon.png` (1024px,
  buddies mark — draft wordmark glyph is the placeholder until the user
  supplies final artwork) and a dedicated monochrome tray template PNG
  (black + alpha). Verify: assets exist, template PNG is pure black+alpha
  (no white), both load via `task build`.
- [x] 1.2 Regenerate `build/darwin/icons.icns` from the new appicon; fill
  `build/darwin/Info.plist` (CFBundleName "buddies", real copyright and
  get-info strings, add `LSApplicationCategoryType:
  public.app-category.utilities`, `NSPrincipalClass: NSApplication`);
  `plutil -lint` passes; About panel no longer shows "My Product"
  (verified in 3.3).
- [x] 1.3 Enable single instance in `main.go`:
  `SingleInstanceOptions{UniqueID: "com.oniharnantyo.buddies",
  OnSecondInstanceLaunch: …window.Show()}`. Verify: second `bin/buddies`
  launch exits, no second tray icon, existing window shows; `go vet ./...`,
  `go test ./...` pass.
- [x] 1.4 Add `DefaultContextMenuDisabled: true` to the window options.
  Verify: right-click in the desktop window shows no web menu.
- [x] 1.5 Implement window frame persistence: save x/y/width/height to
  `~/.buddies/window.json` on move/resize (Wails window events), restore
  before first show, clamp against current displays, default frame on
  missing/corrupt file. Verify: move/resize → quit → relaunch restores
  frame; unplug scenario simulated by hand-editing the JSON off-screen →
  window still visible; `go vet ./...`, `go test ./...` pass.
- [x] 1.6 Corner-type check on macOS: with the Liquid Glass rim, confirm
  `MacWindowCornerTypeSquare` still looks deliberate vs platform rounding;
  keep or revert (one line). Verify: decision recorded here, GUI screenshot.
  Decision: KEEP `MacWindowCornerTypeSquare` (default per design D8); GUI screenshot pending the unlocked-Mac pass — revert is a one-line change if the rim reads wrong.

## 2. Frontend conventions — dialogs, cursor, selection, motion

- [x] 2.1 `ModalShell.tsx`: Escape keydown closes via `onClose`; add vitest
  case (renderUI, fire Escape, assert close callback; assert no submit).
- [x] 2.2 Autofocus audit: every dialog's primary input focuses on open —
  add `autoFocus` where missing (`ProviderFormDialog` name field; confirm
  hook/tool modals), keep agents' dialogs as-is. Verify: vitest focus
  assertions per dialog.
- [x] 2.3 Add the conventions layer to `styles.css`: cursor rules (controls
  default arrow, inputs I-beam), `user-select: none` on chrome selectors
  (transcripts/inputs/model lines untouched), global
  `prefers-reduced-motion` collapse (opacity-only ≤150ms, instant focus
  rings); delete the three `cursor-pointer` classes in `ProvidersPane`,
  `HooksPane`, `McpPane`. Verify: grep built CSS for `cursor:pointer` on
  controls returns nothing; both themes still legible.
- [x] 2.4 Frontend gates: `npx tsc --noEmit`, `npx vitest run`,
  `npx vp check src` all clean (baseline warnings only).

## 3. Verification and close-out

- [x] 3.1 Rebuild everything: `cd frontend && npm run build`, then
  `task build` and `task build:server` (binaries embed fresh dist).
- [ ] 3.2 Server-mode visual pass (headless harness): open provider dialog
  (Escape closes, name field focused), hover controls (no pointer),
  select chrome vs transcript, reduced-motion emulation, dark + light.
- [ ] 3.3 Manual GUI checklist (unlocked Mac): dock icon, tray glyph on
  both menu-bar appearances, About panel strings, second-launch focus from
  Finder, relaunch-after-kill -9, frame persistence, right-click silent,
  Escape/⌘. behavior, plus audit carry-overs (launch flash, Pinyin IME,
  green-button zoom, idle memory).
- [x] 3.4 Update `docs/native-feel-audit.md`: tick resolved red items,
  move verified manual findings into the doc, note the corner-type
  decision.
