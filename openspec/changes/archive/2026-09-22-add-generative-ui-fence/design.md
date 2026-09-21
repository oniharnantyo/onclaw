# add-generative-ui-fence — Design

## Context

The 14 fence tags render one card each; composition is impossible. The `@assistant-ui/generative-ui` element is already vendored (shadcn install: element + 157-selector style registry + `@assistant-ui/react-generative-ui`/`zod`/`remark-gfm` deps). A nine-thread exploration (2026-09-21) established the adoption shape; a live-model probe (glm-4.7) supplied real failure data. The in-flight implementation in the working tree already carries most of this change; these tasks verify, complete, and extend it.

Key prior facts (verified, see memory `generative-ui-real-examples-explore`):
- The vocabulary's `properties` fields are real zod schemas with `safeParse`; the renderer itself validates nothing.
- `safeParse` strips unknown keys by default and rejects enum/range/required violations — covering every trap found (icon set, size enum, gap/padding range, invented props).
- Upstream quirks found live: the registry style item ships a four-dash typo (`var(----aui-success)`), `.dark`-class theming that this app never sets, and 9 raw shadcn token names (`--card`, `--foreground`, …) absent from our tokens.
- A live model wrote `"type"` instead of `"$type"`, `gap: 16` (pixel thinking), an invented `icon` prop, and `Caption {text}` (schema requires `value`).

## Goals / Non-Goals

**Goals**
- A `ui` fence that renders model-composed trees through the trimmed vocabulary, with schema-gated validation.
- Failure semantics that match the fence philosophy: strict at the boundary, tolerant inside.
- The model's two observed failure modes (`type`, pixel gaps) tolerated instead of punished.
- Failure observability: the user can tell a degraded card from ordinary code.
- The theming mismatch between shadcn installs and our token system becomes test-enforced.

**Non-Goals**
- The forms lane (`$action` → run resume) — the approval seam is boolean-only today; its extension is a separate change.
- `Image` — deferred until a URL policy exists (model-chosen `<img src>` is a tracking/exfiltration surface).
- Streaming-into-card behavior — unchanged; fences render complete (D2 of markdown-card-elements).
- Replacing the flat tags — they stay the default; `ui` is the composition layer only.

## Decisions

### D1 — Schema-driven validation; the hand-written walker is deleted

`uiOf` walks the tree and gates each node through its component's `properties.safeParse`. Enums (`Icon.name`, `Icon.size`, `Chart.variant`), ranges (`gap`/`padding` 0–8), and required props (`Text.value`, `Button.label`, …) come from the schemas; `safeParse` strips unknown keys by default, so invented props can never reach a renderer. The hand-written icon set, token-range checks, and enum checks are deleted — they duplicated (and drifted from) the schemas and missed required-prop failures entirely.

Coupling accepted: the validator now depends on the package's schema internals. Mitigated by the same vendoring that already imports the element, and by a capability test pinning that every entry exposes `safeParse`.

### D2 — Root-strict, interior-tolerant failure semantics

- Root node fails → `uiOf` returns null → the fence degrades to the captioned source block (D2 of markdown-card-elements).
- Interior node fails → that node drops; siblings render.

Rationale: rejecting the whole fence on one bad caption turned a 400-char composition into 400 chars of raw JSON (thread g measured the degraded blast radius). Interior drops keep the composition readable; the missing piece is actionable user feedback ("the labels didn't render").

### D3 — `type` accepted as `$type`

A live glm-4.7 turn wrote `"type"` in its first `ui` fence — `$`-prefixed keys are not a universal convention, and the failure degraded the entire composition. The walker reads `$type` first, then `type`. No component declares a `type` prop, so the fallback cannot shadow one. The prompt doc still teaches `$type`.

### D4 — Trimmed vocabulary: 24 of 27

`uiLibrary` (exported from the element) filters `styledGenerativeUILibrary` to a 24-name allowlist. Excluded: `Image` (security — model-chosen `<img src>` fires requests from the user's browser: tracking pixels, query-string exfiltration, internal-host probing), `DatePicker` (forms-lane, inert), `Carousel` (untaught, low value in a chat column). `Chart` and `Table` stay (the user's scope choice): their fences remain the default for data viz, and cross-lane shape confusion fails closed via the schemas (verified: fence-shaped input into `ui.Chart`/`ui.Table` rejects).

The map is the single source of truth: `uiOf` validates through it, the renderer dispatches through it, and the prompt-doc catalog mirrors it. Extending it is a registration-style entry addition (AGENTS.md plugins-are-first-class).

### D5 — `Card.background`: the one hand-written rule

The schema accepts any string, but upstream renders it as an inline CSS value and forces `color:white` — a light value is white text on a white card (a gallery once rendered blank from exactly this). The walker deletes `background` after `safeParse`. No generated validator can know this; it is ours, and it is the only one.

### D6 — Token clamping before validation

`gap`/`padding` numbers are clamped to 0–8 before `safeParse`. Models think in pixels (`gap: 16` for comfortable spacing); upstream silently ignores values above 8 (no CSS rule), so clamping preserves the layout intent where rejection would drop the node (D2 semantics make that drop visible but avoidable).

### D7 — Double-validation avoided; the renderer accepts the validated spec

`parseFence('ui', …)` runs `uiOf` and returns `{ spec }`. The registered renderer accepts `props.spec` directly (falling back to a raw `$type`-bearing tree for the legacy `$type` tool-result lane) and MUST NOT re-run `uiOf` — the wrapper has no `$type`, so re-validation returns null and the fence renders nothing, silently (the bug the end-to-end probe caught; unit tests of the halves passed while the composition was broken).

### D8 — Degraded-fence caption

A failed fence renders its source inside the existing styled block plus a one-line caption ("This card couldn't be rendered — showing source") with the validator reason on the caption's `title`. The reason is already computed by `parseFence` and was previously discarded. Model-facing silence is unchanged for now (surfacing rejections to the model needs a renderer→server channel; deferred — the hooks block-JSON precedent shows the mechanism).

### D9 — Raw shadcn token aliases + the install guard test

The vendored CSS calls raw shadcn names. `theme.css` gains a `@theme inline` block aliasing the missing ones onto contract tokens (`--card: var(--surface)`, `--foreground: var(--fg)`, `--muted-foreground: var(--muted)`, `--primary*: var(--accent*)`, `--destructive: var(--danger)`, `--input: var(--border)`, `--radius: 12px`). Names that ARE contract tokens (`--border`, `--muted`, `--accent`, `--bg`, `--fg`) are never re-declared (self-referential). The install's duplicate `@custom-variant dark (&:is(.dark *))` is deleted (theme.css owns the variant, bound to `data-theme`), its `.dark { --aui-* }` block is re-bound to `:root[data-theme="dark"]`, and its four-dash typo (`var(----aui-success)`) is corrected.

A new guard test makes the class silent-proof: every `var(--x)` referenced in `index.css` must be defined in `tokens.css`/`theme.css`/`index.css`; `@custom-variant dark` appears exactly once, in `theme.css`; no `.dark` selector ships.

### D10 — Prompt budget: universal now, measured decision later

The `ui` subsection costs ~482 tokens on every turn (24% of the instruction payload) and the live model used it zero times unprompted. Decision: keep it universal for now, and re-probe after D3 lands — if unprompted usage rises, the cost is earned; if not, the section moves behind a per-agent/workspace toggle. The worked example stays (the shape is the hardest thing to teach); the catalog is already minimal.

## Risks / Trade-offs

- **Schema coupling** (D1): a package upgrade that reshapes `properties` breaks the validator. Mitigation: the capability test asserting `safeParse` exists on all 24 entries fails loudly at upgrade time.
- **Interior tolerance hides model errors** (D2): a dropped node is a silent partial render. Mitigation: root-strict boundary keeps total failure visible; per-node drops preserve the composition.
- **`type` tolerance widens acceptance** (D3): a tree legitimately using a `type` prop would be misread. Mitigation: no component declares one (verified across all 24 schemas).
- **zod in the main chunk**: +46 kB gzip, measured. Accepted by the adoption decision.
- **shadcn reinstall**: `shadcn add` of the same or another registry:style item re-writes the theming blocks. Mitigation: the D9 guard test fails the reintroduction.

## Migration Plan

No data migration. The new fence tag is additive; existing transcripts render unchanged. The Go prompt-doc test update requires a server rebuild before agents see the `ui` catalog.

## Open Questions

- Whether `ui` earns universality (D10) — decided by post-D3 re-measurement, not by this change.
- Whether `Chart`/`Table` should eventually migrate out of `ui` into fence-tag-only usage (thread d) — deferred; the schemas make cross-lane confusion fail closed, so the decision is not urgent.
