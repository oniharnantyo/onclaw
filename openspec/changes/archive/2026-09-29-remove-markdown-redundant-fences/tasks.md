# Tasks

## 1. Fence universe (`web/src/lib/generativeUi/`)

- [x] 1.1 Remove `table` and `math` from `FENCE_TAGS` in `fences.tsx`; delete their shape validators from `JSON_TAGS`
- [x] 1.2 Delete `FenceMathBlock` and the lazy `katex` import in `fences.tsx`
- [x] 1.3 Remove the `table` and `math` `registerSpecRenderer` calls, the `GENERATIVE_UI_TYPES` entries, and related imports in `index.tsx`
- [x] 1.4 Delete `web/src/components/assistant-ui/elements/data-table.tsx` and `elements/math-block.tsx`

## 2. Tests and fixtures

- [x] 2.1 Update `generativeUi` guard/coverage tests to the 13-tag universe; remove table/math fixture cards
- [x] 2.2 Add a degradation test: a ` ```table `/` ```math ` fence renders an ordinary code block with the degraded caption
- [x] 2.3 Grep web tests and fixtures for residual `table`/`math` fence usage and prune

## 3. Verification

- [x] 3.1 `pnpm test` green; `tsc` clean
- [x] 3.2 Manual: a persisted message with a ` ```table ` fence renders the degraded code block; `$$…$$` math and a GFM markdown table still render natively
