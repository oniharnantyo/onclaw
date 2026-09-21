## 1. Shimmer primitive and shared label

- [x] 1.1 Add the `od-shimmer` CSS primitive to the global stylesheet: animated gradient text sweep via `background-clip: text`, honoring `prefers-reduced-motion: reduce` (static muted text fallback)
- [x] 1.2 Create the shared `ActivityLabel` component (label + optional elapsed + shimmer span keyed on label text so flips replay the sweep; reduced-motion falls back per 1.1)
- [x] 1.3 Unit tests: shimmer span re-keys on label change, elapsed renders only when provided, reduced-motion renders static text

## 2. Fold owns reasoning rows (AgentMessage)

- [x] 2.1 Gate reasoning segments behind the fold on the parts path: `p.k !== 'tool'` segments render only when `showCards` (heavy turns), light turns unchanged
- [x] 2.2 Apply the same gate to the legacy flat-reasoning branch (`ReasoningBubble` behind `showCards`)
- [x] 2.3 Unit tests: collapsed heavy turn renders header + text only (no Thought rows); expanded reveals cards and Thought rows in stream order; light turn renders cards + thoughts inline with no header; legacy flat-reasoning heavy turn hides its thought while collapsed
- [x] 2.4 Expanded fold body nests under the header behind a thin left rail (design D8, user-approved 2026-09-21 visual pass); light turns render without the rail; unit test pins the rail wrapper

## 3. Header activity status (ToolTimelineHeader)

- [x] 3.1 Add the streaming branch to `ToolTimelineHeader`: when `streaming`, render `ActivityLabel` with "Running <display name>" when a pending call exists, else "Thinking"; accept display name + elapsed from the caller
- [x] 3.2 Client-measured turn elapsed in `AgentMessage` (turn-scope clock: start on live render, freeze at run end, `formatLatency`), passed to the header only while streaming
- [x] 3.3 Resting branch unchanged ("N steps · M files changed", steps = tool calls only); no shimmer at rest
- [x] 3.4 Unit tests: pending call shows "Running Shell", no pending call shows "Thinking", completion swaps to the resting summary with no shimmer, hydrated turn renders resting summary without elapsed

## 4. ThinkingRow joins the vocabulary

- [x] 4.1 Replace ThinkingRow's bare pulsing dot with `ActivityLabel` ("Thinking" + elapsed), keeping the avatar row and `role="status"` semantics
- [x] 4.2 Unit tests: waiting row renders the shimmering "Thinking" label with ticking elapsed, avatar and status role preserved

## 5. Verification

- [x] 5.1 `pnpm build` and full vitest suite green in `web/`
- [x] 5.2 Visual pass: live heavy turn (collapsed streaming header flips Thinking → Running → resting; expanded fold shows interleaved cards + thoughts), hydrated heavy turn (resting header, no elapsed), light turn (unchanged), reduced-motion header (static label)
