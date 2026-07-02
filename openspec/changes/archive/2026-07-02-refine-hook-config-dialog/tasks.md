# Tasks

## 1. Reusable Tooltip component

- [x] 1.1 Create `web/src/components/Tooltip.tsx`: Phosphor `Info` icon trigger, popover shown on hover and keyboard focus, `role="tooltip"` + `aria-describedby` + `tabIndex={0}`, styled with existing design tokens; default placement above the trigger.
- [x] 1.2 Add `.tooltip`, `.tooltip-trigger`, `.tooltip-content` styles to `web/src/index.css`.
- [x] 1.3 Add `.form-error` (color `--error`, 11px) and `.form-input.is-invalid` / `.form-textarea.is-invalid` border states mirroring the existing `:focus` ring pattern.

## 2. Validation helpers

- [x] 2.1 Create `web/src/components/hookValidation.ts` with pure validators returning `{ ok: boolean; error?: string; warn?: string }`.
- [x] 2.2 `validateRegex(pattern)`: `new RegExp(pattern)` syntax check (empty = valid); set `warn` for RE2-incompatible constructs (`(?=`, `(?!`, `(?<=`, `(?<!`, backreferences `\1`–`\9`); `ok=false` only on a real syntax error.
- [x] 2.3 `validateCommand(cmd)`: non-empty + balanced-quote parity for `'`, `` ` ``, `"`.
- [x] 2.4 `validateScript(src)`: `new Function(src)` syntax check + `handle`-definition presence (declaration, assignment, arrow, and method-shorthand forms).
- [x] 2.5 `validateTimeout(ms)`: integer in `[1, 10000]`.
- [x] 2.6 If a web test runner is configured in `web/package.json`, add unit tests for the validators (regex valid/invalid/RE2-warn; command balanced/unbalanced; script no-handle/declaration/arrow/method-shorthand; timeout range).

## 3. Hooks.tsx — guidance + fixed placeholder

- [x] 3.1 Add a `<Tooltip content={…}/>` next to each `.form-label` using the field copy in the design (events, scope, timeout, timeout policy, priority, matcher, handler type, command, cwd, env vars, JS source).
- [x] 3.2 Add per-option `title` hints to the lifecycle-event and handler-type `<select>` options.
- [x] 3.3 Fix the JavaScript textarea placeholder to the real `function handle(ctx)` → `{decision, reason}` contract using `ctx.*` fields.

## 4. Hooks.tsx — inline validation + block-on-error

- [x] 4.1 Add an `errors` state object keyed by field, recomputed on change via the `hookValidation` helpers.
- [x] 4.2 Render `<span className="form-error">` under a field when its error is set; add `is-invalid` class plus `aria-invalid`/`aria-describedby` on the input.
- [x] 4.3 In `handleAddHook`, run `validateAll()`; on any hard error, set errors and return without POSTing. Disable the Save button while a hard error is present.

## 5. Hooks.tsx — Beautify + Test

- [x] 5.1 Add `js-beautify` (+ `@types/js-beautify`) to `web/package.json`.
- [x] 5.2 Add a **Beautify** button next to the JS source label (script handler only): `setScript(js_beautify(script, { indent_size: 2 }))`, re-run script validation, disabled when empty, wrapped in try/catch with a toast on failure.
- [x] 5.3 Extract `buildHookPayload()` (shared by Test and Save) mirroring the current POST body and handler-specific `config` JSON.
- [x] 5.4 Add a **Test** button in `.modal-footer`: run client validation first (abort on hard error), then `POST /api/hooks/test`; show the returned `decision` as a badge and any `error` text inline above the footer; reset the result on any field change.

## 6. Verification

- [x] 6.1 `cd web && <pkg-manager> install` (add `js-beautify`/`@types/js-beautify`), then build (e.g. `npm run build` / `pnpm build`); expect zero TypeScript errors.
- [x] 6.2 Manual exercise of the dialog (Vite dev server or `make build && ./bin/onclaw serve`): tooltips appear on hover/focus; invalid regex/JS/command show inline errors and disable Save; RE2 lookahead warns; Beautify reformats minified JS and keeps it valid; Test shows decision/error inline; Save still creates the hook when all validations pass.
- [x] 6.3 Run `openspec validate --changes refine-hook-config-dialog` (or `openspec verify --changes refine-hook-config-dialog`) and confirm it passes.