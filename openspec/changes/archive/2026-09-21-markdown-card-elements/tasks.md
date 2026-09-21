## 1. Frontend foundation: shadcn + token seam

- [x] 1.1 Initialize shadcn in `web/` (components.json, Tailwind v4 + Vite paths) without altering the existing design-token definitions
- [x] 1.2 Add the Tailwind v4 `@theme inline` aliasing block mapping shadcn token names onto OnClaw contract tokens (`--color-background: var(--bg)`, `--color-foreground: var(--fg)`, `--color-border: var(--border-line)`, `--color-popover: var(--surface)`, `--color-accent: var(--accent)`, plus radius/font mappings), in both light and dark themes
- [x] 1.3 Install the element registry items (`elements-data-table`, `elements-number-ticker`, `elements-activity-graph`, `elements-spec-sheet`, `elements-comparison-card`, `elements-job-progress`, `elements-score-breakdown`, `elements-flow-graph`, `elements-math-block`, `elements-diagram`, `mermaid-diagram`, `shiki-highlighter`; shared `elements-surfaces`, `elements-range` arrive as dependencies) and commit the vendored sources
- [x] 1.4 Review `elements-surfaces` recipes against the design contract; restyle in that one file where the aliasing block alone is insufficient (radii, fonts, motion)
- [x] 1.5 Decide and wire `lucide-react` vs the in-house `Icon` for the two cards that want it (design D4); record the choice in the task notes
- [x] 1.6 Add dynamic-import wrappers for the mermaid engine and the shiki highlighter with degraded first paint (plain code block / raw source) and in-place upgrade; unit tests with mocked loaders

## 2. Fence transport: registry + renderer

- [x] 2.1 Extend `generativeUi/registry.tsx` with `renderSpecByType(type, props, ctx)` reusing the registered renderers; grow the `GENERATIVE_UI_TYPES` coverage-guard set with the new tags
- [x] 2.2 Write per-tag validators: required fields, enums (`variant`, `state`, flow states), non-empty arrays, preview URL absolute http(s), `recommendedId` ∈ options, math steps, mermaid/diagram raw-source bodies; return typed props or a rejection
- [x] 2.3 Write the model→element adapters: derive/drop presentational props (`visibleCount`, `cycle`, `onCancel`), map math LaTeX strings through the KaTeX lane, map activity data to heat-graph datapoints
- [x] 2.4 Generalize the installed data table to `{columns: [{key, label}], rows: [object]}` with an optional caption, keeping its styling language (design D5)
- [x] 2.5 Extend the `pre` override in `AgentMessage.tsx`: recognized tag + valid body → `renderSpecByType` mount (passing the `busy && isLast` live latch); any failure → existing styled code block; inline code unaffected
- [x] 2.6 Wire `mermaid`/`diagram` mounting through the lazy mermaid wrapper (`diagram` inside the catalog zoom chrome) and `math` through the math-block element
- [x] 2.7 Wire ordinary code blocks through the lazy shiki highlighter (theme follows app light/dark, plain while streaming, copy control); plain-code fallback when the engine or language is unavailable
- [x] 2.8 Renderer tests: each catalogue tag mounts its element inline; malformed JSON / bad preview URL / unknown `recommendedId` / unknown tag degrade to a code block; unclosed streaming fence stays plain; legacy `$type` envelope tests keep passing; lazy engines mocked

## 3. Backend: prompt injection + tool removal

- [x] 3.1 Add the rich-cards section to the embedded `promptdocs/AGENTS.md` template: one line per fence tag with its shape, plus the "cards only when a visual beats prose; JSON must be valid" rules
- [x] 3.2 Switch `DefaultInstructionComposer.Compose` and `trimmedUnattendedDocs` to inject `promptdocs.BasePrompt` directly (drop the `readPromptFile(dir, "AGENTS.md")` reads); extend composer tests (attended, scheduler, heartbeat) for injection and the rich-cards section
- [x] 3.3 Update `promptdocs.SeedWorkspace` to stop writing `AGENTS.md` (keep directory creation and generated-doc clearing); update promptdocs tests
- [x] 3.4 Add the startup sweep (alongside the system-skills sync) that deletes stray seeded `AGENTS.md` files from existing agent workspaces, with logging of each removal; cover with tests including "generated documents untouched"
- [x] 3.5 Delete `internal/agents/tools/ui_chart.go`, `ui_timeline.go`, `ui_preview.go` and their tests; remove the three registrations from `tool_registry.go` and catalog entries from `tool_catalog.go` (keep `strictDecodeArgs` — todo.go uses it)
- [x] 3.6 Verify stale allowlist behavior: unknown keys stay inert through create/update/execute (extend or point at the existing silent-drop tests)

## 4. Frontend cleanup

- [x] 4.1 Remove the three tools from `web/src/lib/toolCatalog.ts` (names, icons) and `toolDisplay.ts` (`CATALOG_TOOL_KEYS`, one-liners, `timelineEventFact`, field specs); update `toolDisplay.test.ts` fixtures
- [x] 4.2 Rename/reframe the `ToolCall.test.tsx` `$type` envelope describe as the legacy-path pin; confirm the unknown-`$type` silence invariant still holds

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 5.2 Web test suite green (vitest), including the coverage guards (tool catalog mirror, `GENERATIVE_UI_TYPES`)
- [x] 5.3 Startup sweep live check: existing dev agent workspace loses its seeded `AGENTS.md`; fresh agent creation seeds no `AGENTS.md`; composer carries the rich-cards section in a real run
- [x] 5.4 Live chat pass: a turn asking for a trend renders the chart card inline; fences for table, timeline, mermaid, and math mount; a deliberately malformed fence degrades to a code block; an ordinary `go` code block highlights with a copy button
- [x] 5.5 Visual pass against the design contract (light and dark): card styling, code-block styling, no layout regressions in the message column at 360px and 1920px
