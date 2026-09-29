# Tasks

## 1. Client todo plan selector

- [ ] 1.1 Add a session todos selector to the client store: derive the current plan from the newest `todo_write` call args across the session's loaded events via the exported `parseTodoPlan`, exposed as `{items, revision}` or null when no plan. Verify with unit tests over synthetic event sequences: single call, cross-turn rewrites, cleared list (empty items → null).
- [ ] 1.2 Feed the selector from the existing tool-call event stream subscription so `todo_write` events update it live. Verify with a store test: streaming a rewrite updates the selector without reload.

## 2. Chip and popover components

- [ ] 2.1 Build the header chip: present-only (renders nothing when the selector is null or the agent does not expose `todo_write`), active-item text in spinner state, done/total count. Verify with vitest component tests for each presence/state branch.
- [ ] 2.2 Build the popover anchored to the chip using the existing popover primitives: header with agent and revision, one row per item reusing `TodoChecklistCard` row states (done struck+dimmed, active spinning, pending dimmed, failed danger + reason), dismiss control and outside-click close. Verify with vitest: open shows the full current list including a failed item's reason; dismiss and outside-click close it.
- [ ] 2.3 Degrade the popover to a bottom sheet below the breakpoint where it cannot fit; no horizontal overflow at any viewport. Verify with vitest at the 360×800 floor plus a manual resize pass.

## 3. Auto-surface behavior

- [ ] 3.1 Wire auto-surface: popover opens on the run's first `todo_write` event, collapses on run finish or no-open-items, stays closed after a user dismissal for the rest of the run, re-arms next run. Verify with vitest interaction tests covering all four behaviors.

## 4. Transcript revision collapse

- [ ] 4.1 Extend the generative-ui todo renderer so only each turn's final `todo_write` renders a full card; earlier calls — cross-turn as well as same-turn — collapse to the one-line plan-updated summary. Verify with vitest over a transcript fixture with multiple turns and rewrites.

## 5. Verification

- [ ] 5.1 Full web suite green: `pnpm -C web test` passes with the new tests included.
- [ ] 5.2 Live pass in the dev environment: agent with `todo_write` exposed — chip appears with progress, popover opens on first plan write, auto-collapses on completion, bottom sheet at narrow width, transcript shows turn-final cards only.
