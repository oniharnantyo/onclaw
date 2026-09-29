# Proposal

## Why

The chat markdown body already renders GFM tables (`remark-gfm`) and LaTeX math (`remark-math` + `rehype-katex`, lazily loaded) natively, so the `table` and `math` fence cards duplicate forms markdown already handles better: a model writes a markdown table or interleaved `$$` blocks with prose between steps, which is more flexible than the cards' fixed chrome (caption bar; label + step-note pairing). Every fence tag costs prompt-teaching tokens and validation surface; tags redundant with native markdown are pure overhead. Decision confirmed with the maintainer 2026-09-26 (table = the trigger; math = audit finding; `spec`, `ticker`, `compare` reviewed and kept).

## What Changes

- **BREAKING (model-facing)**: remove the `table` fence tag — registration, `columns`/`rows` body validator, `data-table.tsx` element, and catalogue entry. Tabular data is served by native markdown tables.
- **BREAKING (model-facing)**: remove the `math` fence tag — registration, `steps` body validator, `math-block.tsx` element and its lazy KaTeX pairing. Math is served by native `$`/`$$` LaTeX rendering (which stays).
- Update the coverage-guard sets (`FENCE_TAGS` / `GENERATIVE_UI_TYPES`) and guard tests to the 13-tag universe: chart, timeline, preview, ticker, activity, spec, compare, progress, score, flow, mermaid, diagram, ui.
- The `ui` composition vocabulary keeps its `Table` component — inside a composition tree there is no markdown lane, so it is not redundant.
- Prompt-side teaching of these two tags disappears as part of `enhance-agent-base-prompt` (which ships the 13-tag end-state catalogue with a markdown-first clause); this change is web-side only. A bare ` ```mermaid ` block still routes through shiki as ordinary code — the `mermaid`/`diagram` fences remain the only diagram lane.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/generative-ui`: the fence body validation enumeration and the card element catalogue drop the `table` and `math` tags; the catalogue collapses to 13 tags.
