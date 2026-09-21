# markdown-card-elements

## Why

The `ui.chart` / `ui.timeline` / `ui.preview` echo tools (adopt-assistant-ui-elements D3) are a heavy transport for display-only cards: they cost tool-schema tokens, per-agent opt-in management, and a tool-call round trip per card, all to move data the model could write directly into its reply. Separately, the L1 base prompt (`AGENTS.md`) is seeded into each agent workspace as a write-once file, so platform guidance can never reach agents created before a template change — the "vintage" divergence that design D11 already routes around for the memory tool. This change replaces the echo-tool transport with prompt-guided markdown fences mounted by the existing react-markdown pipeline, and moves the L1 base prompt to per-build injection so platform guidance (including the new card conventions) reaches every agent on every prompt build.

## What Changes

- **BREAKING (tool surface)**: Remove the three generative-UI echo tools (`ui.chart`, `ui.timeline`, `ui.preview`) — registrations, catalog entries, and their Go implementations. Stored agent configs referencing them become inert (unknown allowlist keys are dropped silently).
- Add the markdown-fence card transport: a system-prompt section (the "rich cards" doc) teaches every agent to emit tagged code fences (` ```chart `, ` ```timeline `, …) with JSON (raw mermaid source for `mermaid`/`diagram`); the chat's react-markdown `pre` override detects the tag, schema-validates the body, and mounts the card. Any invalid body degrades to the ordinary code block — never a crash, never a blank.
- Retain the `$type`-from-tool-result dispatch in the generative-UI registry as a legacy rendering path for transcripts produced by the removed tools.
- Add card elements built to the assistant-ui elements-catalog specs, installed via the shadcn registry (`@assistant-ui/elements-*`), restyled to OnClaw contract tokens: `table` (generalized from the model-usage-hardcoded data table), `ticker`, `activity`, `spec`, `compare`, `progress`, `score`, `flow`, `math`, plus `mermaid` and `diagram` (mermaid source rendered as SVG; `diagram` wraps the render in zoom chrome). Chart, timeline, and preview keep the existing OnClaw cards.
- Upgrade all ordinary code blocks with shiki syntax highlighting (lazy-loaded, theme mapped to the app's light/dark themes, plain rendering while the fence streams, copy button).
- Inject the L1 base prompt per build from the embedded template instead of reading the seeded per-agent file; stop seeding `AGENTS.md` as a workspace file and remove already-seeded copies with a startup sweep. IDENTITY/SOUL/BOOTSTRAP/HEARTBEAT remain workspace files.
- Heavy dependencies (`mermaid`, `shiki`) load via dynamic import on first use, matching the existing KaTeX lane.

## Capabilities

### New Capabilities

- `web-app/generative-ui`: Markdown-fence card rendering in the chat transcript — fence tags, per-shape validation, degradation to plain code blocks, the card element catalogue, lazy heavy engines, and legacy envelope rendering for old transcripts.

### Modified Capabilities

- `agent-runtime`: "Instruction composition" — the L1 base prompt is injected per build from the embedded template (no longer read from the seeded workspace file), and the composed stack carries the rich-cards guidance section in attended, scheduler, and heartbeat profiles.
- `agent-prompts`: "Prompt documents are workspace files" — `AGENTS.md` is no longer seeded into agent workspaces; a startup sweep removes already-seeded copies. IDENTITY/SOUL/BOOTSTRAP/HEARTBEAT seeding is unchanged.
- `workspace-tools`: "Tool catalog" — the catalog no longer offers `ui.chart`, `ui.timeline`, or `ui.preview`.

## Impact

- **Backend**: `internal/agents/tools/` (delete three tools + tests), `tool_registry.go`, `tool_catalog.go`, `runner.go` (composer: inject `promptdocs.BasePrompt` per build in attended + unattended profiles; rich-cards section), `internal/promptdocs/` (embedded AGENTS.md gains the rich-cards section; seeding stops writing it; startup sweep removes stray seeded copies), fixtures in `runner_todo_test.go`, `context_breakdown_test.go`.
- **Frontend**: `web/src/components/chat/AgentMessage.tsx` (fence interception in the `pre` override), `web/src/lib/generativeUi/` (`renderSpecByType`, new element components, validators), `web/src/lib/toolCatalog.ts` / `toolDisplay.ts` (remove the three tools), Tailwind v4 token aliases mapping shadcn-standard tokens onto OnClaw contract tokens, `components.json` (shadcn init), new npm deps (`mermaid`, `shiki`, registry-shared items).
- **Sequencing**: `adopt-assistant-ui-elements` (complete, unarchived) archives first; its generative-ui and workspace-tools deltas land for the elements the tools built. This change's `web-app/generative-ui` requirements are ADDED with distinct names, and its `workspace-tools` delta replaces the catalog requirement so the removed trio lands only transiently. No database migration; the stray-file sweep is a filesystem startup pass.
