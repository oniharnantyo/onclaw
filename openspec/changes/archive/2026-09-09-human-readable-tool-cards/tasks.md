## 1. Formatter foundation

- [x] 1.1 Create `web/src/lib/toolDisplay.ts`: types (`ToolFormat` with `oneLiner {intent, outcome, object?, facts?}`, field specs `{key, label, kind}` with kinds chip/quote/content/enum/plain) and the declarative table covering all 12 catalog tools (memory split by `action` variant) and all 10 browser facade members — per design.md D1
- [x] 1.2 Implement one-liner resolution: state machine over the card item (running → intent, done → outcome + facts, error → intent styled as error; unparseable/absent args → name only) per D2, with the trusted-envelope facts (web.search `results.length`, write size from args `content`)
- [x] 1.3 Unit tests: one-liner per state for representative tools (memory append/read, shell, web.search, browser.click), irregular verbs (Write→Wrote), facts appending only at done, parse-fail fallback

## 2. Catalog mirror + translator touch

- [x] 2.1 Extend `web/src/lib/toolCatalog.ts` to mirror `icon_key` from the tools catalog next to `display_name`, with a default icon for MCP ids (design D8)
- [x] 2.2 In `TranscriptTranslator` (`web/src/lib/livechat.ts`) tool_call_started push, store `ts: ev.occurred_at` on the card (one-line addition, design D7)

## 3. Card rendering

- [x] 3.1 Rework `ToolCall.tsx` collapsed header: catalog icon per tool, one-liner from toolDisplay (chips/quotes styled per kind), latency/error styling as today; name + dots when no one-liner can mint
- [x] 3.2 Expanded view: labeled field rows from the tool's field specs with present-only rendering; kind-shaped values; wall-clock `ts` row; unknown arg keys appended humanized (D3)
- [x] 3.3 Clamp component: shared "first N lines + show all N lines" wrapper used by content blocks, diffs, and text results (D3)
- [x] 3.4 Raw toggle in every expanded card showing the unmodified args/result strings (D6); add `aria-expanded` to the toggle button
- [x] 3.5 Result shaping: JSON object → humanized key-value rows, array → list, plain text → clamped block, empty → "No output returned." (D4)
- [x] 3.6 Special renderers: `edit_file` stacked before/after diff (clamped); `web.search` numbered title+host result list; snapshot envelope → role/name/ref element table; ref-action results → "Snapshot refreshed — N elements"
- [x] 3.7 ref→name lookup: sibling-card walk over the same agent message's tool cards (latest snapshot envelope first) resolving click/type/select names, refs as chips when unnamed (D5) — requires passing sibling cards into ToolCall
- [x] 3.8 Approval card: render the command via the shell formatter's chip styling so approval → resolved-execute cards read continuously (D7)

## 4. Generic fallback + coverage guard

- [x] 4.1 Generic arg/result fallback for non-catalog tools: humanized keys (reuse the MCP acronym casing logic), conservative value kinds (http(s) → url chip, long strings clamped), raw text when JSON doesn't parse (D4)
- [x] 4.2 Coverage guard test: every catalog key + facade member has a table entry (prevents silent drift when tools are added)

## 5. Verification

- [x] 5.1 Run vitest on touched suites (`ToolCall`, `toolCatalog`, `livechat`, transcript translator tests) — note ~66 pre-existing jsdom/localStorage failures exist repo-wide at clean HEAD; judge only touched suites + `pnpm build`
- [x] 5.2 `pnpm build` green
- [ ] 5.3 Manual browser pass: live chat turn exercising memory append, shell command, file edit, web search; then reload to verify hydrated cards render identically to live ones
