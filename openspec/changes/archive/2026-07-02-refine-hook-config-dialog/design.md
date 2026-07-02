# Design — refine-hook-config-dialog

## Goals

- Make every field in the Configure New Hook dialog self-explanatory.
- Catch invalid regex/command/script input before it reaches the server.
- Expose the existing dry-run endpoint so users can verify a hook before saving.
- Fix the incorrect JavaScript placeholder so copied examples work.

## Non-Goals

- Changing hook backend behavior, the dispatcher, handlers, storage, or the REST API.
- Extending `POST /api/hooks/test` to accept a caller-supplied payload (the endpoint already fabricates a sample payload; richer per-input testing is deferred).
- Introducing a web test runner or a component library.

## Key Decisions

### 1. Regex validation must be RE2-aware (server is authoritative)

The server validates matchers with Go's `regexp.Compile` (RE2): no lookahead, look-behind, or backreferences. A naive client check using `new RegExp(...)` runs on the browser's V8 engine and can accept patterns the server rejects. Decision: the client runs `new RegExp(pattern)` for syntax, and additionally emits a warning when the pattern contains RE2-incompatible constructs (`(?=`, `(?!`, `(?<=`, `(?<!`, backreferences `\1`–`\9`). Syntax errors block save; RE2 warnings are advisory. The server remains the source of truth.

### 2. JavaScript validation is a first pass; the Test button is authoritative

The script handler runs in goja (≈ES5.1 + some ES6). The browser's `new Function(src)` parses with a modern engine, so it can pass scripts goja rejects (e.g. optional chaining `?.`, nullish coalescing `??`). Decision: client validation does (a) `new Function(src)` for syntax and (b) a regex presence check for a `handle` definition covering `function handle(`, `handle = function`, arrow forms, and method shorthand. This catches the common errors (typoes, missing `handle`). The authoritative compile/run check is the Test button, which POSTs to `POST /api/hooks/test` where the server compiles with `goja.Compile` and runs `handle(ctx)` against a sample payload.

### 3. Command validation stays lightweight

Real shell parsing is a rabbit hole and error-prone. Decision: non-empty plus a balanced-quote heuristic (parity of `'`, `` ` ``, `"`). No AST, no shell grammar. The Test button provides real validation for command handlers.

### 4. Beautify via js-beautify, not Prettier

Decision: use `js-beautify` (`js_beautify(code, { indent_size: 2 })`). It is synchronous, token-based (tolerates minor malformation so it still indents usable code), and far smaller than Prettier standalone — fitting the project's lean-bundle ethos. The button reformats the textarea contents in place, re-runs script validation, is disabled when the source is empty, and is wrapped in try/catch so it never throws on bad input.

### 5. Tooltip as a small reusable component

No tooltip component exists; the codebase uses plain CSS with design tokens and Phosphor icons, and `.form-hint` for help text. Decision: add a dependency-free `Tooltip` component (`Info` icon trigger, popover on hover and keyboard focus, accessible via `role="tooltip"`/`aria-describedby`/`tabIndex={0}`), styled with existing tokens. Reusable on table headers later.

### 6. Share payload construction between Test and Save

Both the Test and Save actions must build the same `store.Hook` body (including handler-specific `config` JSON). Decision: extract a `buildHookPayload()` helper used by both, avoiding drift. Test additionally surfaces `{decision, error?}` from the dry-run response inline; Save preserves the existing `POST /api/hooks` flow.

## File Layout

- `web/src/components/Tooltip.tsx` (new) — reusable tooltip.
- `web/src/components/hookValidation.ts` (new) — `validateRegex`, `validateCommand`, `validateScript`, `validateTimeout`.
- `web/src/components/Hooks.tsx` (modified) — tooltips on labels, fixed JS placeholder, inline `form-error` display + block-on-error, Beautify button, Test button + result line, `buildHookPayload` helper.
- `web/src/index.css` (modified) — `.tooltip*`, `.form-error`, `.form-input.is-invalid` / `.form-textarea.is-invalid`.

## Risks

- **Tooltip clipping inside a 550px modal.** Mitigation: default placement above the label; switch per-field if a tooltip clips against the modal edge.
- **Client regex/JS checks diverging from the server.** Mitigation: client checks are best-effort with explicit warnings; the Test button and server-side `regexp.Compile`/`goja.Compile` are authoritative.
- **Bundle size.** Mitigation: `js-beautify` is the only new dependency and is small; validation uses native browser APIs.