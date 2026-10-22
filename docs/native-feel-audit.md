# Native-Feel Audit — buddies desktop

**Date:** 2026-10-16 · **Scope:** macOS desktop app (`main.go`, window/tray/menu
wiring, frontend shell) · **Method:** the `native-feel-cross-platform-desktop`
skill — decision tree, 75-item ship-readiness list, WebView survival guide,
native-conventions checklist. Scored ✓ observably correct / ✗ failed / ◯
unverified or N/A.

**Context notes:** buddies is Wails3 (Go native shell + system WKWebView), which
maps onto the skill's philosophy — native below the rendering surface, shared
UI above, generated bindings as the one schema. All background work (gateway,
cron, webhooks) lives in Go, below the WebView, so WebKit throttling cannot
affect it. The island container grammar (design.md, amended 2026-10-16) landed
*after* this audit and invalidates none of its items.

## Decision-tree verdict

Proceed. Multi-OS trajectory (macOS today, Windows/Linux later), native feel is
a product requirement (tray utility), launch/memory budgets generous, runway
long. The Raycast four-layer stack (Node + Rust + UniFFI) does not apply —
Wails3 already provides the equivalent layering with Go as the backend runtime.
Wails delegates part of the native-shell layer to the framework: acceptable,
but it means shell-level fixes must go through Wails options/hooks (cited
below where one exists).

## Section scores

| Section | Score | Notes |
|---|---|---|
| A. Cold launch | 2✓ 4✗ 4◯ | icon/manifest placeholders dominate |
| B. Window & focus | 2✓ 3✗ 5◯ | no window-state persistence |
| C. Input & cursor | 2✓ 5✗ 3◯ | pointer cursor, context menu, Escape |
| D. Visual & material | 4✓ 1✗ 5◯ | materials strong; reduced-motion partial |
| E. Scrolling | 3✓ 2◯ | clean — WebKit defaults untouched |
| F. Performance | 1✓ 6◯ | verify 10s timeout ✓; rest needs measurement |
| G. System integration | 1✓ 3✗ 6◯ | single-instance off, bundle wrong |
| H. Accessibility | 5◯ | unverified (needs manual/VoiceOver pass) |
| I. Cross-platform parity | 5◯ | N/A until Windows/Linux builds |

## Red items (fixable, no design conflict)

1. **Dock/tray icon is the Wails placeholder.** `build/appicon.png` is the
   default "W" logo; it feeds the dock icon, the tray (template icon,
   `main.go` `setupTray`), and `build/darwin/icons.icns`.
2. **`build/darwin/Info.plist` is the untouched template.** `CFBundleName:
   "My Product"`, `CFBundleGetInfoString: "This is a comment"`,
   `NSHumanReadableCopyright: "© 2026, My Company"`. The About panel reads
   these. Missing: `LSApplicationCategoryType`, `NSPrincipalClass`.
3. **Single instance not enabled.** `application.Options.SingleInstance`
   exists in wails v3.0.0-beta.29 (`SingleInstanceOptions{UniqueID,
   OnSecondInstanceLaunch}`) and is unset — a second launch means two tray
   icons and two processes contending for `~/.buddies/buddies.db`.
4. **WebKit context menu is live.** Right-click shows the browser menu
   (Reload/Inspect). Wails has `DefaultContextMenuDisabled: true`
   (`webview_window_options.go`) — one line in the `WebviewWindowOptions` in
   `main.go`.
5. **Modals don't answer Escape.** `ModalShell.tsx` has no keydown handling;
   the only Escape handler in the app is the composer's mention menu. No
   dialog closes on Escape (skill item 29: Escape must always do something).
6. **`cursor: pointer` everywhere.** 17 rules in the built CSS (daisyUI
   preflight/components) + 3 explicit `label cursor-pointer`
   (`ProvidersPane.tsx`, `HooksPane.tsx`, `McpPane.tsx`). The skill's single
   most "feels like a web app" tell.
7. **No window size/position persistence.** Wails beta.29 does not persist
   window frames; needs manual save on move/resize (Wails window events) into
   the settings store + restore before show.
8. **Dialog focus management.** Only `CreateAgentModal`/`DeleteAgentModal`
   autofocus an input; `ProviderFormDialog` and others open with no focused
   control.
9. **Text selection on chrome.** Only the header bar is `select-none`
   (`App.tsx`); sidebar labels and menu rows select like a document.
10. **`prefers-reduced-motion` partially honored.** Only `.card-lift`
    collapses (`styles.css`); the glass/daisyUI transitions don't.
11. **Corner type.** `main.go` sets `CornerType:
    MacWindowCornerTypeSquare`, opting out of the platform's rounded window
    corners. Possibly deliberate for the glass rim — verify, don't assume.

## Resolution — `polish-native-feel` applied 2026-10-16

Items 1–10 are fixed and machine-verified (openspec `polish-native-feel`,
verifier PASS on fresh gates: gofmt/vet/test, tsc/vitest/vp check, plutil,
`task build`, `task build:server`); item 11 is decided pending a GUI look.

1. **Icon** — replaced with a coral "b" draft mark: `build/appicon.png`
   (1024²), regenerated `build/darwin/icons.icns` (pixel-identical to the
   appicon), and a dedicated 44×44 pure black+alpha tray template
   `build/appicon-tray.png` wired via `SetTemplateIcon` (`main.go`). Stale
   `build/darwin/Assets.car` (old "W" renditions, unregenerable without the
   Icon Composer toolchain) was deleted and `CFBundleIconName` removed from
   both plists so macOS falls back to `CFBundleIconFile` → icons.icns. The
   `.icon` source package is updated to the new mark for when real tooling
   regenerates it; final artwork still owed by the user.
2. **Info.plist** — truthful strings (`buddies`, real copyright/get-info),
   `LSApplicationCategoryType: public.app-category.utilities` and
   `NSPrincipalClass: NSApplication` added; `plutil -lint` clean on both
   plists.
3. **Single instance** — `SingleInstanceOptions{UniqueID:
   "com.oniharnantyo.buddies", OnSecondInstanceLaunch → window.Show()}` in
   `main.go` (second launch exits, existing window re-shown). Relaunch after
   `kill -9` stays on the manual checklist.
4. **Context menu** — `DefaultContextMenuDisabled: true` in the
   `WebviewWindowOptions`.
5. **Escape** — `ModalShell` keydown effect: Escape closes every modal
   (vitest-covered), backdrop click unchanged.
6. **Pointer cursor** — global conventions layer in `styles.css` (controls
   arrow, inputs I-beam) and the three explicit `label cursor-pointer`
   tokens deleted.
7. **Window frame persistence** — `~/.buddies/window.json` via the new
   stdlib-only `internal/desktop` package (Load/Save/Clamp, unit-tested):
   restored before first show (born in place), saved on move/resize,
   clamped against current displays; missing/corrupt file → default frame.
8. **Dialog focus** — `autoFocus` on the primary input of
   `ProviderFormDialog`, the hooks modal, and the MCP modal (+ focus test);
   agents' dialogs were already correct.
9. **Chrome selection** — `user-select: none` on chrome selectors;
   transcripts, inputs, `pre`/`code`, and `.font-mono` model lines stay
   selectable.
10. **Reduced motion** — global `prefers-reduced-motion` collapse
    (opacity-only ≤150ms, instant focus rings), superseding the
    `.card-lift`-only block.
11. **Corner type** — decision: KEEP `MacWindowCornerTypeSquare` (design D8
    default); confirm against the Liquid Glass rim in the manual GUI pass —
    reverting is one line.

New process note from verification: generated frontend bindings can go
stale (tsc/vitest fail on a fresh tree); `task build` regenerates them —
always run the Taskfile pipeline before the frontend gates.

## Deliberate deviations — keep (locked decisions)

- Self-hosted fonts (Jakarta/Inter/JetBrains) instead of the system font —
  offline requirement + locked Hallmark design.
- Coral brand accent instead of the system accent — locked design.
- App theme toggle drives the NSWindow appearance (not system-following) —
  user's explicit FINAL call in the liquid-glass session.
- Settings in-window (⌘, reveals + navigates) instead of a native settings
  window — user reversed the popup decision.
- Glass DOM modals instead of NSAlert — part of the design system.
- Hover tints on controls — the skill permits this when varied by control
  kind, which daisyUI's are.

## Manual checklist (needs the unlocked Mac)

- First-launch white/black flash before first paint (A2).
- Pinyin IME composition in the composer (C26).
- Green button zooms instead of fullscreen (B13).
- Focus rings visible and platform-styled (C28).
- Idle memory (<500 MB) and background CPU after an hour (F46/F49/F50).
- Quit leaves no zombie process (A10) — tray Quit → `OnShutdown` path.
- Streaming updates arriving while the window is hidden, then re-shown
  (no `visibilitychange` refetch exists; WKWebView may throttle timers —
  events should still deliver, verify a live transcript catches up).

## Suggested next step

`polish-native-feel` is applied and verified (see Resolution above). What
remains is exactly the manual checklist below plus the change's own
GUI pass: dock icon, tray glyph on both menu-bar appearances, About panel
strings, second-launch focus from Finder, relaunch after `kill -9`, frame
persistence across relaunch and after editing `window.json` off-screen,
silent right-click, Escape/⌘, behavior, and the corner-type screenshot.
