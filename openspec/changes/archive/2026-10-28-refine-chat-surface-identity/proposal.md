# Proposal

## Why

A dual-agent design critique (25/40) and technical audit (13/20) of the chat
surface found the center of the product missing the identity its positioning
promises: sessions are indistinguishable untitled rows, assistant turns are
anonymous text, failed turns end on a raw Go error string, and rail state is
invisible to assistive tech. The recently rebuilt session rail and right bar
carry the product's companions-with-reach character; the rest of the surface
does not yet match them.

## What Changes

- Session rows regain a last-activity relative-time meta line (right-aligned
  on the existing 26px row), restoring the scannability the compact-row
  redesign dropped.
- Each assistant turn opens with the session agent's avatar and name — a
  static stub; the right bar's live avatar remains the single live face.
- Failed turns render a human error card: a classified plain-language
  headline, the raw error preserved in a collapsed technical-details
  disclosure, and a Retry action that regenerates the turn (the failed
  attempt becomes variant 1 under the existing branch rules).
- Composer rail distill: the model trigger shows the model's family name
  (full id stays in the picker menu and the tooltip), the `/ @ $` hint row
  renders only while the draft is empty, and the right bar adopts the same
  family-name model line — ending the two-formats-for-one-model drift and
  the mid-token wrap seen at ordinary widths.
- Rail semantics: `SideRow` emits `aria-current` for the active row and
  `aria-expanded` for accordion headers; the rail's icon buttons meet the
  24px target floor.
- Context-ring percentage label moves to the lifted accent tier (3.81:1 →
  5.87:1 on dark); the warn and danger tiers follow.
- The desktop window gains an enforced minimum size, measured against the
  distilled composer rail.
- Structural chrome moves as one system: the nav rail's collapse, the
  session column's reveal, session-group accordion expand/collapse, the
  right bar's minimize/restore, and the identity↔preview swap animate on
  the shared motion tokens; drag-resize stays glued to the pointer, and
  reduced motion keeps its existing global collapse.

## Capabilities

### New Capabilities

<!-- none — no new capability introduced -->

### Modified Capabilities

- `chat-view`: the sessions requirement's entries carry a last-activity time;
  an ADDED requirement pins per-turn assistant attribution in the transcript;
  an ADDED requirement pins error presentation with recovery; and an ADDED
  requirement pins the composer rail's economy — the family-name model
  trigger, a hint row that yields to any non-empty draft, and a truncating
  right-bar model line.
- `ui-conventions`: ADDED requirements pin interactive-state semantics
  (selection/expansion announced, 24px target floor), structural
  transition motion (one shared token grammar for chrome, grid-row
  accordion reveal, drag stays untransitioned), and status text contrast
  (status text on the lifted accenttext/warntext/dangertext tiers, 4.5:1 in
  both themes, strokes on the raw fills) at the kit level.
- `app-shell`: the window-frame requirement gains a minimum-size floor so
  resize cannot squeeze the composer below its working width.

## Impact

- `frontend/src/features/chat/components/SessionRail.tsx` — meta line,
  `expanded` pass-through, target sizes (meta line + group `expanded`
  already prototyped in the working tree, unverified).
- `frontend/src/components/ui/SideRow.tsx` — `aria-current`/`aria-expanded`
  (prototyped in the working tree, unverified).
- `frontend/src/features/chat/thread/ChatAssistantMessage.tsx` — turn
  identity stub, hardened error card.
- `frontend/src/features/chat/composer/ChatComposer.tsx`,
  `ComposerPickers.tsx` — hint yield, family-name trigger label.
- `frontend/src/features/chat/composer/ComposerContext.tsx` — tier classes.
- `frontend/src/features/chat/components/RightBar.tsx` — model line;
  collapsed state becomes keep-mounted so minimize/restore animates.
- `frontend/src/App.tsx` — nav rail column transition.
- `main.go` — `MinWidth`/`MinHeight` on the desktop window options.
- Frontend test suites for the touched surfaces; no backend, no bindings
  regen (the retry rides the existing regenerate path; error classification
  is a frontend bridge with `classify-turn-errors` as the named follow-up
  that retires it).
