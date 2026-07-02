## Why

The Web UI's Configure New Hook dialog exposes a powerful lifecycle-hook system but gives users no per-field guidance and almost no input validation. Concepts like blocking vs. non-blocking events, fail-closed timeout policy, priority ordering, and the RE2-only tool matcher are unexplained, so hooks are hard to configure correctly. Worse, the JavaScript handler placeholder is wrong: it references a `Decision()` function and a `payload` global that do not exist in the goja sandbox, so any user who copies it produces a hook that always fails with "script must define a 'handle(ctx)' function". Errors surface only as a late toast after a failed save, and the existing `POST /api/hooks/test` dry-run endpoint is not exposed in the dialog at all.

## What Changes

- Add a contextual tooltip to every field in the Configure New Hook dialog (events, scope, timeout/policy, priority, matcher, handler types, command exit-code semantics, env allowlist, and the JS `handle(ctx)` contract), plus per-option `title` hints on the event and handler-type selects.
- Add inline, real-time validation that blocks Save on hard errors:
  - **Regex matcher**: syntax check via the browser, plus an RE2-aware warning when the pattern uses constructs Go's `regexp` rejects (lookahead/look-behind, backreferences), since the server validates matchers with RE2.
  - **Command**: non-empty plus a balanced-quote heuristic (no shell parsing).
  - **JavaScript source**: syntax check via `new Function`, plus a check that the source defines `handle(ctx)` per the backend contract.
  - **Timeout**: integer in the 1–10000 ms range.
- Fix the JavaScript placeholder to the real contract: `function handle(ctx)` returning `{decision, reason}` against the `ctx.*` event fields.
- Add a **Beautify** button (via `js-beautify`) that reformats the JS source in place.
- Add a **Test** button that POSTs the in-progress hook to the existing `POST /api/hooks/test` dry-run endpoint and shows the returned decision (and any error) inline before saving.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-ui`: the hook configuration dialog gains spec-level requirements to validate handler inputs before submission (regex/command/script/timeout, with the correct JS contract), to offer a dry-run test using `POST /api/hooks/test`, and to provide per-field guidance so each option is understandable.

## Impact

- **Modified files**: `web/src/components/Hooks.tsx` (tooltips, fixed JS placeholder, inline validation + block-on-error, Beautify and Test buttons, shared `buildHookPayload` helper); `web/src/index.css` (tooltip and form-error styles).
- **New files**: `web/src/components/Tooltip.tsx` (reusable tooltip component); `web/src/components/hookValidation.ts` (pure validators).
- **New dependency**: `js-beautify` (+ `@types/js-beautify`) added to `web/package.json`. Regex and JS-syntax checks use native browser APIs (`RegExp`, `Function`).
- **No backend changes**: the dialog reuses the existing `POST /api/hooks/test` endpoint, which compiles the script with goja and runs it against a sample payload.