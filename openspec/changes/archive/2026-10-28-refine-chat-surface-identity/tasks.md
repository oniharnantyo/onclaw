# Tasks

## 1. Rail identity and semantics (SessionRail + SideRow)

- [x] 1.1 SideRow emits `aria-current` from `active` and optional
      `aria-expanded` (prototyped in the working tree, unverified —
      assertion lands in 1.4)
- [x] 1.2 Session rows carry the right-aligned `relativeTime(LastActivityAt)`
      meta line in the SideRow `sub` slot, 26px density preserved
      (prototyped, unverified — assertion lands in 1.4)
- [x] 1.3 Group header passes `expanded`; group-`+` and delete targets are
      24px (hit area, not glyph) (prototyped; delete button still 20px in
      the tree — finish it)
- [x] 1.4 Tests: SessionRail suite asserts meta line per row (pinned clock),
      `aria-expanded` on the group header, `aria-current` on the active
      row, and 24px targets; `npm run test -- SessionRail` green

## 2. Turn attribution (ChatAssistantMessage)

- [x] 2.1 Static 16px AgentAvatar + agent-name stub renders above each
      assistant turn's content (below the turn rule, after compaction
      early-return); no second `useLiveAvatar` instance anywhere
- [x] 2.2 Tests: chatThread suite asserts the stub names the session agent
      on assistant turns and is absent on compaction markers

## 3. Error presentation (ChatAssistantMessage)

- [x] 3.1 ErrorLine becomes the human card: classified plain-language
      headline (provider unreachable / credentials / context window /
      honest fallback), raw string in a collapsed mono details disclosure,
      alert role preserved
- [x] 3.2 Retry button regenerates via the message's existing reload path
      (branch rules; hidden while the turn is running)
- [x] 3.3 Tests: entry-error renders headline + details + retry; retry
      triggers regenerate; raw string absent from the headline but present
      in the disclosure

## 4. Composer distill (ChatComposer + ComposerPickers + RightBar)

- [x] 4.1 `shortModelName` helper (last `/` segment) in chat `format.ts`;
      model trigger label uses it with the full id as title; picker menu
      keeps full ids. Live smoke at 1100px proved the family label still
      wrapped mid-token: the vendored trigger is now styled through its
      `className` seam as an ellipsizing block (chevron pinned, longest-
      label-first shrink ladder), verified one line at 1100 in both themes
      (`.team/smoke/refine-chat-surface-identity/2026-10-28/16-…`, `17-…`)
- [x] 4.2 The `/ @ $` hint row renders only while the composer draft is
      empty — no text AND no attachment chips; trigger popovers unchanged
- [x] 4.3 RightBar model line adopts provider-name + family label,
      truncate (not break-all), full id on hover
- [x] 4.4 Tests: composer tests assert family label, hint yield on typing,
      and the right-bar line; existing pickers/queue suites stay green

## 5. Ring contrast (ComposerContext)

- [x] 5.1 Tier text classes move to `accenttext`/`warntext`/`dangertext`;
      strokes unchanged; ComposerContext suite green. `text-warntext` was
      inert (no token): `--warn-text` now exists in both themes
      (`#a16207` light 4.92:1, `#fbbf24` dark 10.43:1) and `theme.css`
      maps `--color-warntext`; a token-contract test guards the mapping

## 6. Window floor (main.go)

- [x] 6.1 Measure the distilled rail at both side bars' minimum drag widths;
      set `MinWidth`/`MinHeight` in the desktop window options to the
      measured floor — 1040×600, arithmetic at the options literal; cold
      launch (no saved frame) is also born at the floor, so a first run
      can never persist a sub-floor frame; `gofmt`/`go vet`/`go test ./...`
      green
- [x] 6.2 Relaunch clamp machine-verified (native, 2026-10-28): a saved
      below-floor frame `800×400` relaunches at `1039×634` (1040×600 floor
      + titlebar, 1039 = point rounding), stable across 35 samples/run,
      3/3 runs; `window.json` re-persists the clamped frame and an
      above-floor `1400×900` restores unchanged. **Live drag-to-shrink
      below the floor remains human-only** (host Accessibility permission
      denied; no synthetic window resize possible) — the AppKit min-size
      constraint is set from the same constants.

## 7. Structural transition motion (App.tsx + SessionRail + RightBar)

- [x] 7.1 Nav rail collapse/expand and session column reveal animate on
      `--motion-base` + `--ease-standard`; drag-resize handlers disable the
      transition while dragging and re-arm on release
- [x] 7.2 Session-group accordion reveals via `grid-template-rows`
      0fr↔1fr (the `LIST_MAX_CLASS` scroll region keeps working); the
      group chevron's 200ms rotation stays in sync with the reveal.
      Rows stay mounted through both states (bounded by each group's
      load-more window) so collapse animates too; a collapsed group's
      rows are `inert` + `aria-hidden`, not focusable or announced
- [x] 7.3 RightBar collapsed state becomes zero-inline-size keep-mounted
      (replaces the `return null` unmount) so minimize/restore animates;
      identity↔preview swap replays `od-fade` on the shared motion grammar
      (`--motion-base` + `--ease-standard`), with no per-instance duration
      override; the collapsed fold is `inert`, and drag re-arms on
      `pointercancel` as well as release
- [x] 7.4 Tests: collapse/restore preserves the identity content; reduced-
      motion path completes without spatial animation (global contract,
      opacity-only; no extra branch); inert round trip and drag-cancel
      re-arm asserted; App/SessionRail/RightBar suites green

## 8. Wave gate

- [x] 8.1 Full frontend gates: `npm run test` (114 files / 1166 tests),
      `npx vp check src` (0 errors), `npm run build`; Go gates
      (`gofmt`/`go vet`/`go test ./...`) green
- [x] 8.2 Impeccable detector over the changed targets: no new
      failure-severity findings (source scan of the 8 changed files: 0
      findings; full-page scan: 3 warnings, all pre-dating this change)
- [x] 8.3 Live smoke (server-mode surface — the real backend + built
      frontend; `task dev` deliberately not used because its binding
      regeneration dirties tracked files): dark + light rail/composer at
      1100px (relative times visible, composer rail one line, no mid-token
      wrap after the 4.1 fix), a real failed-turn card (headline + Retry +
      collapsed details with the raw `dial tcp … connection refused`), the
      motion pass (nav rail 224→80→224, right bar 280→0→280, accordion
      reveal 159→761px, mid + settled frames), plus transcript attribution
      from the earlier evidence. Artifacts and numbers:
      `.team/smoke/refine-chat-surface-identity/2026-10-28/`
      (`smoke.md`, `measurements.json`, `railfix-measurements.json`, PNGs)
- [x] 8.4 `openspec validate refine-chat-surface-identity --strict` passes;
      archive after `add-grouped-session-rail` (design D7) — grouped rail
      archived as `2026-10-28-add-grouped-session-rail` first, then this
      change (`2026-10-28-refine-chat-surface-identity`); delta merge
      verified in the main specs
- [x] 8.5 Close-out defects found by smoke, fixed and re-verified: warn
      text token; `od-fade` shared grammar; accordion close; RightBar
      drag-cancel + inert; composer no-wrap at 1100; cold-launch floor;
      error-card Retry stand-down (adapter maps failed turns to
      incomplete, so the store's busy flag is the reachable signal)
