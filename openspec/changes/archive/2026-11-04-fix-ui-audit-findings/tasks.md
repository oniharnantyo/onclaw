# Tasks

## 1. Contract status tiers

- [x] 1.1 Add `--warn-text` (light `#b45309`, dark `#eab308`) and `--success-text` (light `#15803d`, dark `#17a34a`) as explicitly defined raw tokens in `tokens.css` — no aliases of the fills — and map `--color-warntext` / `--color-successtext` in the `theme.css` `@theme` block; extend the theme vitest with a token-contract test computing WCAG ratios for all four lifted tiers on `--surface`/`--bg` in both themes, asserting ≥ 4.5:1
- [x] 1.2 Amend DESIGN.md: token-inventory rows for the two new tiers with the lifted-tier wording, and the status-hue sentence "status hues reach the UI only through contract tokens; palette utilities must not encode status colors, vendored surfaces included"; verify the doc's token names and values match `tokens.css` exactly

## 2. Status palette re-base

- [x] 2.1 Re-base first-party status colors: `ChatToolGroup.tsx` emerald/red pairs → `text-successtext`/`text-dangertext` (+ fill tokens for chips/strokes), `RunSurfaces.tsx` emerald-500/600 icon and text pairs → success tokens per D2 sizing rule, composer warn lines `ChatComposer.tsx:413,423` → `text-warntext`; verify with the component tests plus a rendered light-theme check of the composer warning against 4.5:1
- [x] 2.2 Re-base vendored `components/assistant-ui/elements/*` status classes (context-usage, tool-error, approval-card, message-queue, reasoning-effort, code-diff, tool-timeline, task-card, subagent-list, message-actions) onto the token families per the D2 map — mechanical class swaps only; extend the token-contract test to grep first-party + `elements/` for `(emerald|red|amber|blue)-[0-9]` classes and fail on any hit
- [x] 2.3 Fix the two detector advisories: vendored `typing-indicator.tsx` dots switch to the kit `.od-dot` classes (drop Tailwind `animate-bounce`), and `DocumentConfigDialog.tsx:984` `border-b-2` → hairline `border-b`; verify `impeccable detect` no longer reports them

## 3. ARIA conformance sweep

- [x] 3.1 Replace `aria-selected` on the button-role lists (`agents/index.tsx:227`, `ProvidersPane.tsx:528`) with `aria-pressed`, keeping the existing Enter/Space handling; update the pinned tests and assert the selected state is announced
- [x] 3.2 Wire the five tab strips (settings nav, `AgentConfigPane`, `IdentitySection`, `DocumentConfigDialog`, right-bar preview tabs) to the shared pattern from D7b: tab ids + `aria-selected` + roving tabindex + arrow-key/Home/End movement, panels with `role="tabpanel"` + `aria-labelledby`; extract the shared hook, unit-test it, and add one interaction test per strip
- [x] 3.3 Make the resize separators keyboard-operable (`SessionRail.tsx:812-817`, `RightBar.tsx` handles): `tabIndex={0}`, start/end arrow keys adjust by the 4px drag increment, Home/End to clamps, persisting like a drag; component test simulates key events and asserts the width persists
- [x] 3.4 Give StatusDot a text state: `aria-hidden` dot + visually-hidden label (and `role="status"` where the state flips live, e.g. running); update the kit test to assert the accessible text
- [x] 3.5 Fix form labeling: `AvatarPicker` trait labels get `htmlFor`/`id`, swatches get human-readable names + `aria-pressed`, Customize disclosure gets `aria-expanded`; `KnowledgePanel.tsx:301-309` description input and the combobox search input get `aria-label`s; tests assert each name survives typing
- [x] 3.6 Repair headings: `ChatHeader.tsx:91-96` title span → `<h1>` with identical visual classes; `AgentCard` h3 → h2; `AgentConfigPane`/`HooksPane`/`ChannelsPane` h4s → h3; tests assert heading reachability per view and no skipped levels
- [x] 3.7 Replace `aria-label`-on-generic-div loading states with `aria-busy` on the transcript and list containers (`ChatThread.tsx:100-106`, agents, scheduler, knowledge); tests assert busy state appears while loading and clears after
- [x] 3.8 Add the field focus treatment: composer shell gets `focus-within` accent border (textarea keeps its input-exception), per D9; verify in both themes by tabbing
- [x] 3.9 Point session-group headers' `aria-controls` at the existing content-region ids (`SessionRail.tsx:510-513` → `:607`); test asserts the pairing

## 4. Transcript live region

- [x] 4.1 Put `role="log"` on the transcript viewport wrapper and flip `aria-busy` while a turn streams (`ChatThread.tsx:176-179`); test asserts the live-log role, and a manual DOM check confirms streamed text lands inside it

## 5. Streaming hot path

- [x] 5.1 Coalesce text-delta ingest in `store.ts`: pending buffer keyed by entry, one commit per animation frame, synchronous flush on turn-terminal events, `eventBuffer` replay untouched; unit test — N deltas within one frame produce one commit and `turn_completed` flushes immediately
- [x] 5.2 Switch the runtime adapter to per-entry conversion (stable per-message objects via the `convertMessage` lane, memoized by entry identity); unit test — a delta preserves object identity of unchanged entries, and the built repository is equivalent to the old builder on a sample transcript; delete the fallback path in the same task
- [x] 5.3 Make `stabilizeMarkdown` incremental (stabilize only the tail since the last stable boundary); unit tests assert parity with the full scan on the existing fence/emphasis corpus
- [x] 5.4 Narrow the whole-`entries` subscribers: RunSurfaces and ContextMeter derive from scalar/id-list slices, `useLiveAvatar` derives from the active turn's terminal-state edge; selector-stability test proves a delta no longer re-derives them

## 6. Bundle surgery

- [x] 6.1 Move beautiful-mermaid behind a lazy boundary: `ChatAssistantMessage` imports `MermaidDiagram` dynamically, the diagram element loads its renderer on mount with a code-fence placeholder; component test renders a mermaid fence end-to-end; build grep confirms the library's markers are absent from the entry chunk
- [x] 6.2 Break the markdown static chains (`reasoning.aui`, `task-card.aui`, `panelRenderers` → `MarkdownBody` via the lazy path) or, if a chain cannot break without behavior risk, delete the dead `lazy()` in `ChatAssistantMessage` and record that markdown rides the entry bundle; verify with build grep + entry-size measurement
- [x] 6.3 Add `manualChunks` vendor groups (react, radix+base-ui, assistant-ui, markdown stack) in `vite.config.ts`; verify `npm run build` splits them, entry chunk lands ≤ ~3.5 MB raw, and the full vitest suite stays green

## 7. Polish tail

- [x] 7.1 Extract the Modal's focus cycle into a shared `useFocusTrap` and use it in `Lightbox` and the vendored image/mermaid zoom dialogs; unit test the trap, and Modal's own suite confirms unchanged behavior
- [x] 7.2 Replace the run card's `backdrop-blur-xl` (`RunSurfaces.tsx:245`) with a solid `--surface` treatment at contract opacity + ring; verify no `backdrop-filter` remains on the card and it `transition`s only opacity
- [x] 7.3 Grow the sub-24px icon targets to a 24px hit area with glyph size unchanged: composer chip retry/remove (`ChatComposer.tsx:150,160`), combobox clear (`Combobox.tsx:354`), right-bar preview-tab close (`RightBar.tsx:505`); tests assert the hit-target sizes

## 8. Wave gate

- [x] 8.1 Run the full gate: `npm run test`, `npm run build` (entry ≤ ~3.5 MB, mermaid/shiki absent from entry), `gofmt -w .`, `go vet ./...`, `go test ./...` (no Go changes expected), `impeccable detect --json` over the changed targets reporting none of the three prior findings
- [x] 8.2 Live smoke via the running app: light-theme composer warning legibility, streaming smoothness on a long session, mermaid fence renders after its chunk loads, DOM spot-check of `role="log"`, tab wiring, and separator keyboard resize; capture screenshots under `.team/smoke/fix-ui-audit-findings/`
- [x] 8.3 Re-run `$impeccable audit` over all UI and record the new score alongside 14/20 in the change notes
