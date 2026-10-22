# Tasks

## 1. Shell — strip and rail (frontend/src)

- [x] 1.1 Replace the desktop spacer strip in `App.tsx` with the title-only
  strip: view-driven title + context line ("Chat" / "3 sessions · 2 running",
  "Settings" / pane summary), text-only, 40px, clear of the traffic lights.
  Keep the below-`lg` header island unchanged. Verify: strip shows no
  interactive elements (test asserts no `button` inside the strip); title
  flips when the nav store switches views.
- [x] 1.2 Add the rail collapse: header row with the panel toggle in the rail
  island; collapsed 52px icon-only width for nav + settings/theme cluster;
  persist via `buddies.railCollapsed` localStorage; restore on load. Verify:
  vitest toggle case (width class/labels hidden), persistence case
  (reload restores state), both themes legible.
- [x] 1.3 Hide the Sessions island and chat-level chrome while Settings is
  open and let the settings view own the work area (D4). Verify: vitest —
  `menu === "settings"` renders no session column; returning to chat keeps
  the active session.

## 2. Sessions island (frontend/src/features/chat)

- [x] 2.1 Restyle `SessionRail` into the Sessions island: `glass-chrome
  island` column, header ("Sessions" + new-session `+`), entries with title +
  relative-time/model meta, active entry in the primary tint. Drop the
  in-panel agents list (shell rail owns navigation). Verify: existing
  SessionRail/ChatView tests updated and green.
- [x] 2.2 Add the search pill: client-side title filter, clearing restores
  the full list, active session unaffected. Verify: vitest filter cases
  (match, no match, clear).
- [x] 2.3 Frontend gates: `npx tsc --noEmit`, `npx vitest run`, `npx vp check
  src` clean (baseline warnings only).

## 3. Settings presentation (frontend/src/features/settings)

- [x] 3.1 Add the `PaneHeader` primitive (components/): title + top-right
  `actions` slot, one row; use it for the settings content pane. Verify:
  Providers renders "Add provider" beside the title; a pane without actions
  renders the title alone.
- [x] 3.2 Add `lib/brandMarks.tsx`: provider-kind → inline SVG map
  (Anthropic clay, OpenAI monochrome) with initial-glyph fallback tile;
  render the tile in provider rows ahead of name/model/status. Verify: vitest
  row renders logo for anthropic/openai kinds and fallback otherwise; both
  themes legible (manual pass).

## 4. Verification and close-out

- [x] 4.1 Rebuild: `cd frontend && npm run build`, then `task build` (desktop
  embeds fresh dist); `task build:server` unaffected-but-green.
- [x] 4.2 Server-mode visual pass (headless harness): three-column chat,
  collapsed rail, settings full island, provider logos, dark + light.
- [ ] 4.3 Manual GUI checklist (unlocked Mac): strip drag behavior (window
  drags from the empty strip; no dead buttons possible), rail collapse on a
  real click (top-left rail header sits below the 50px band), agent switching
  after the Sessions island change, Pinyin IME in the new search field.
- [x] 4.4 Update the shell-layout memory/handoff: tick the converged design
  as scaffolded and record any deviations from the preview.
