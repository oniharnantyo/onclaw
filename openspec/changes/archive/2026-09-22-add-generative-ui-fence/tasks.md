# add-generative-ui-fence — Tasks

## 1. Validator and vocabulary

- [x] Vendor the element (`styledGenerativeUILibrary`) and register the `ui` fence tag in `FENCE_TAGS`
- [x] Export `uiLibrary` from the element: the 24-component trim (drop `Image` for security, `DatePicker` and `Carousel` as untaught/inert)
- [x] Schema-driven `uiOf`: walk the tree; gate each node through `uiLibrary[$type].properties.safeParse`; unknown keys stripped by the schema
- [x] Root-strict / interior-tolerant failure semantics: a failing root rejects the fence; a failing interior node drops while siblings render
- [x] `type` accepted as an alias of `$type` on every node (no component declares a `type` prop)
- [x] `gap`/`padding` clamped to 0–8 before validation (models think in pixels)
- [x] `Card.background` deleted after parse — the one hand-written rule (schema-valid, renders white-on-white)
- [x] Depth (12) and node-count (400) guards
- [x] The registered renderer accepts parseFence's `{spec}` wrapper without re-validating (double-wrap fix: re-validation rejected the wrapper and the fence rendered nothing)
- [x] Add `ui` to the registry coverage guard (14 → 15 tags)

## 2. Prompt documentation

- [x] L1 template gains the `ui` subsection: node protocol, worked example, component catalog with constraint ranges, and the selection rule (single tags stay the default; `ui` only when arrangement carries meaning)
- [x] `promptdocs_test.go` pins the new markers (tree shape, sparing-use rule, token range, icon set)

## 3. Failure observability

- [x] Degraded fences render a one-line caption ("This card couldn't be rendered — showing source") below the source block, with the validator's rejection reason as the caption's hover title
- [x] Successfully mounted cards and ordinary code blocks carry no caption
- [x] The existing degraded-fence tests still pass (plain-pre classing, meta-as-source fallback)

## 4. Theming guard (shadcn install class)

- [x] Guard test: every `var(--x)` referenced in `index.css` resolves against tokens.css/theme.css/index.css
- [x] Guard test: `@custom-variant dark` appears exactly once, in theme.css, bound to `:root[data-theme="dark"]`; no `.dark` selector ships
- [x] Raw shadcn token aliases in theme.css (`--card`, `--foreground`, `--muted-foreground`, `--primary-foreground`, `--accent-foreground`, `--destructive`, `--input`, `--radius`) — the vendored CSS called names this app never defined
- [x] Install bugs fixed: duplicate `.dark`-bound dark variant removed; upstream four-dash typo (`var(----aui-success)`) corrected; `.dark` aui overrides re-bound to `data-theme`

## 5. Verification

- [x] tsc clean; full web suite at baseline; production build green
- [x] Go: `promptdocs` tests green (template markers); `go build ./...` green (embedded template changed → server rebuild)
- [x] Live browser pass: a real model turn produces a `ui` fence; the composition renders in light and dark; a deliberately malformed `ui` fence degrades to the captioned source block
- [x] Token-budget re-probe recorded (does unprompted `ui` usage appear once the `type` tolerance ships? — informs whether the L1 section stays universal)

## 6. Hygiene

- [x] Delete exploration artifacts from the tree: `web/overlapprobe.mjs`, `web/trimprobe.mjs`, `web/src/lib/generativeUi/__e2e.test.tsx` (promote its capability pins into a named test file first)
- [x] `openspec validate add-generative-ui-fence --strict`
