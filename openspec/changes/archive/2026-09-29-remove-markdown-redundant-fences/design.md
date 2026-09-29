# Design

## Context

The chat markdown body (`AgentMessage.tsx`) already lazy-loads `remark-math` + `rehype-katex` for native `$`/`$$` LaTeX and renders GFM tables via `remark-gfm`. The `table` fence (validator + `data-table.tsx`) and `math` fence (validator + `FenceMathBlock`/`math-block.tsx`) duplicate those forms with fixed chrome. The fence universe is pinned in `fences.tsx` (`FENCE_TAGS`, 15 entries) and mirrored by the coverage-guard set `GENERATIVE_UI_TYPES` in `index.tsx`.

## Goals / Non-Goals

**Goals:**
- Shrink the fence universe to 13 tags by deleting the two markdown-redundant mounts end-to-end (tag list, validators, renderer registrations, vendored elements, guard tests, fixtures).
- Keep the degradation contract intact: an unknown/removed tag degrades to an ordinary code block, never a crash.

**Non-Goals:**
- Removing the `ui` vocabulary's `Table` component (inside composition trees there is no markdown lane).
- Touching the native math lane or GFM table rendering in `AgentMessage.tsx`.
- Prompt-side edits (the 13-tag catalogue with the markdown-first clause ships via `enhance-agent-base-prompt`).

## Decisions

- **D1 — Delete the vendored element files with their mounts.** `data-table.tsx` is imported only by the `table` mount; `math-block.tsx` only by the `math` mount (`index.tsx` + `fences.tsx`). No shared consumers (verified by grep 2026-09-26), so both files go.
- **D2 — Removal order inside the file: tag list → validator → registration → element → tests.** The guard tests (`GENERATIVE_UI_TYPES` coverage) are updated in the same commit so the universe change cannot ship half-done.
- **D3 — Removed tags degrade as unknown.** Because `FENCE_TAGS` no longer contains them, `parseFence` rejects with `unknown fence tag` and `renderPre` degrades to the styled code block plus the degraded caption — the existing D2 contract covers old persisted messages without any migration.
- **D4 — Rejected: keeping `math` for its label/notes chrome.** Interleaved `$$` blocks with prose between steps express the same content natively and render before the fence lane would.

## Risks / Trade-offs

- Persisted historical messages containing ` ```table `/` ```math ` fences will re-render as degraded code blocks with the "couldn't be rendered" caption. Accepted — the caption explains the state, and the raw source stays readable.
- `seed.ts` / test fixtures containing table or math cards need pruning (grep-verified none in `seed.ts` today; tests have some).

## Migration Plan

None — runtime degradation handles persisted content.

## Open Questions

None.
