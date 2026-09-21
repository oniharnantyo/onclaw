# add-generative-ui-fence — Proposal

## Why

The 14 fence tags render one card each; the model cannot compose cards into a layout, so a "morning briefing" or an incident view ships as stacked blocks that read as separate answers. The `@assistant-ui/generative-ui` vocabulary (already vendored by the shadcn install) supplies the missing composition layer — containers, typography, status surfaces, and forms — and its per-component zod schemas supply a validation contract we would otherwise hand-maintain. A live-model probe (glm-4.7) proved both the demand and the hazards: the model can emit valid composition trees when asked, but writes `type` instead of `$type`, thinks `gap` is pixels, and invented props must be stripped — so adoption needs a schema-gated fence validator, not a raw pass-through.

## What Changes

- **New `ui` fence tag** carrying a `{$type, ...props}` composition tree, rendered through the vendored `styledGenerativeUILibrary` (`renderGenerativeUI`).
- **Trimmed vocabulary (24 of 27)**: `Image` excluded for security (model-chosen `<img src>` is a tracking/exfiltration surface), `DatePicker` and `Carousel` excluded as untaught/inert. The trimmed `uiLibrary` is the single source of truth for validate + render + the prompt-doc catalog.
- **Schema-driven validator**: each node is gated through its component's `properties.safeParse` (enums, ranges, required props, unknown-key stripping come from the schema, not hand-written guards). `Card.background` is the one hand-written deletion (schema-valid, renders white-on-white).
- **Root-strict / interior-tolerant failure semantics**: a failing root rejects the fence (degrades to the ordinary code block, D2); a failing interior node drops while siblings render.
- **`type` accepted as an alias of `$type`** at every node (live model wrote `"type"`; no component declares a `type` prop, so the fallback cannot shadow one).
- **`gap`/`padding` clamped to 0–8** before validation (models think in pixels; a live turn wrote `gap: 16`, which upstream silently ignores).
- **Degraded-fence caption**: a fence that fails validation renders its source inside a captioned block ("This card couldn't be rendered — showing source") with the validator reason on hover, instead of bare JSON with no signal.
- **Prompt doc**: the L1 template gains the `ui` tag (composition + selection rule: `ui` only when arrangement carries meaning; single tags stay the default).
- **shadcn install guard test**: asserts every `var(--x)` in `index.css` resolves, `@custom-variant dark` appears exactly once (in theme.css, bound to `data-theme`), and no `.dark` selector ships — the class of silent theming bugs this install introduced (duplicate dark variant, `.dark` blocks, 9 unresolved tokens, upstream 4-dash typo).

## Capabilities

### New Capabilities
<!-- none — the composition fence lands inside the existing generative-ui capability -->

### Modified Capabilities
- `web-app/generative-ui`: adds the `ui` composition-fence requirement (schema-gated tree validation, trimmed vocabulary, root-strict/interior-tolerant semantics, `type` alias, token clamping, `background` deletion) and modifies fence body validation to cover it.
- `web-app/chat`: adds the degraded-fence caption requirement (failed fences render a captioned source block carrying the validator reason).
- `agent-prompts`: extends the base-prompt rich-cards requirement with the `ui` tag, its selection rule, and the vocabulary constraints.

## Impact

- **Code (web)**: `web/src/lib/generativeUi/fences.tsx` (schema-driven `uiOf` + tag registration), `web/src/lib/generativeUi/index.tsx` (renderer registration reading the trimmed library), `web/src/components/assistant-ui/elements/generative-ui.tsx` (trimmed `uiLibrary` export), `web/src/components/chat/AgentMessage.tsx` (degraded-fence caption), `web/src/styles/theme.css` + `web/src/index.css` (raw shadcn-name token aliases installed by the vendored CSS).
- **Code (Go)**: `internal/promptdocs/AGENTS.md` + `promptdocs_test.go` (L1 catalog gains the `ui` section; embedded per build → server rebuild required).
- **Dependencies (web)**: `@assistant-ui/react-generative-ui`, `zod`, `remark-gfm` (installed by the adoption; zod enters the main chunk at ~+46 kB gzip).
- **Tests**: fence validator tests (composition semantics), registry coverage guard (14 → 15 tags), new CSS-resolution guard test.
- **Not in scope**: the forms lane ($action → run resume; the approval seam is boolean-only today), `Image` (deferred pending a URL policy), streaming-into-card behavior (unchanged: fences render complete).
