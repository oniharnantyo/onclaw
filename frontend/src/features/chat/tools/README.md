# tools/ — tool card bodies and their support modules

The tool-card module (OpenSpec `enhance-chat-ux`, design D2): `ToolCallCard`
(`../components/ToolCallCard.tsx`) is chrome only — header row, status,
latency, chevron, fold participation — and delegates its body to the
registry here.

## Layout

- `registry.ts` — `REGISTRY_INDEX` (name → body component),
  `resolveBody(name)` returning `FallbackBody` for unknown/MCP names, and
  the body props contract (`ToolBodyProps`) every renderer receives:
  `{ call, args, result, isError, status, durationMs, openPath }`.
- `parse.ts` — `parseArgs(argsText)` and `parseJSONResult(result)`: total
  functions, `null` on garbage (never throw), plus `str`/`bool`/`num`
  defensive field readers and `callDurationMs` (the optional `duration_ms`
  field on the tool record; a zero or absent value — a record persisted
  before durations were recorded — reads as no latency, never a fabricated
  one).
- `toolDisplay.ts` — the sentence grammar (design D3): the intent/outcome
  table for the fifteen builtin tools, the `` `chip` ``/`'literal'` markup
  parser, display-name/icon resolution from the settings catalog (raw-name
  fallback), latency formatting.
- `collapse.ts` — the bounded LRU map (~800, keyed by call id) behind
  card open/closed state and the `edit_file`/`write_file` auto-open-once
  rule with explicit-close precedence (design D9).
- `useReducedMotion.ts` — the live `prefers-reduced-motion` check; the
  running label's shimmer swaps to static muted text under it (design D10).
- `primitives/` — `DiffView` (jsdiff unified diff, design D4), `CodeBlock`
  (the one code surface, design D14), `FileChip`, `Lightbox`, `ClampedText`,
  `RawToggle`.
- `renderers/` — one file per shaped body (`EditFileBody`, `WriteFileBody`,
  `ReadFileBody`, `ExecuteBody`, `GrepBody`, `FileListBody` (glob/ls +
  shared `FileList`), `SearchBody` + `WebFetchBody`, `BrowserBody`
  (including the screenshot thumbnail), `TodoBody`, `KeyValueBody`
  (http_get/current_time), and `FallbackBody` + `BodyBoundary`.

## Contracts

- **Body contract.** Bodies are pure functions of the persisted record (args
  - result), so live and reloaded transcripts render identically. `args` is
    the parsed JSON or `null` — when parsing fails the card renders
    `FallbackBody` directly instead of the shaped body.
- **Degradation.** Every shaped body mounts inside `BodyBoundary`; a
  renderer that throws on a malformed payload degrades that card to
  `FallbackBody` and never blanks the card or the transcript (spec
  "Body rendering failure degrades"). Bodies therefore fail fast on
  wrong-typed required fields rather than rendering half a shape.
- **No inventions.** Unknown and MCP tools mint no sentence and no latency
  that is not in the record; counts in outcome sentences come from the
  delivered result only.
- **Both themes.** Every visual surface rides the token palette
  (`bg-surface`, `border-line`, `color-mix` tints of `--success`/`--danger`)
  — no hardcoded colors anywhere in this module.
