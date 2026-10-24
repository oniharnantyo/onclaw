# Proposal

## Why

The chat view's right bar shows only the agent's identity (avatar, name, model), so opening a workspace file spawns a **second** column beside it instead of replacing the bar's content, and a subagent's trajectory is reachable only as a raw `.tasks/…output` file the parent agent happens to read. Markdown files preview as raw mono text with no rendered view.

## What Changes

- Merge the identity sidebar and the preview panel into **one right bar**: the identity is the bar's empty state; opening any preview replaces it in place (never a second column); closing the last tab returns the identity; the existing collapse toggle hides the whole bar; one width preference replaces the two per-surface widths.
- Make the run card's agents-lane rows clickable: activating a delegated task opens its **trajectory tab** in the bar.
- The trajectory tab reads the task's recorded transcript through the existing jailed workspace reader and renders it like the main transcript — task prompt as a user-style message, text as markdown, reasoning as collapsible disclosures, tool steps as tool cards, the terminal result as the child's final answer — refreshing live while the task runs.
- Markdown workspace files (`.md`/`.markdown`) get a **Preview | Raw** toggle in their preview tab, raw as the default, rendered through the same markdown pipeline the transcript uses.
- Go side: no service/API changes; pin the `<workspace>/.tasks` trajectory-file resolution with a test and correct the stale `TranscriptPath` doc comment (bindings regen).

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `chat-panels`: the right panel shell becomes the shared right bar whose empty state is the session identity; preview sources gain run-card agent rows as a trajectory entry point; new requirements for the subagent trajectory preview and the markdown preview toggle.
- `chat-view`: the Chat right sidebar requirement becomes the identity view of the shared bar — identity shows when no preview is open and is replaced in place otherwise; collapse and resize semantics carry over.

## Impact

- Frontend: `frontend/src/features/chat/components/RightSidebar.tsx` and `RightPanel.tsx` merge into one bar container; `frontend/src/features/chat/index.tsx` mounts one surface; `run/islandModel.ts` carries `transcript_path` on agent rows; `run/RunSurfaces.tsx` wires row activation; a standalone markdown primitive joins `frontend/src/components/assistant-ui/elements/` (reusing the vendored `MarkdownText` component pipeline — no new dependencies).
- Backend: test-only additions in `internal/agents` / `internal/services` for trajectory path resolution; doc-comment fix on `TaskActivity.TranscriptPath` in `internal/sessions` + bindings regen (`task common:generate:bindings --force`).
- Sessions recorded by older binaries whose `.tasks` files no longer resolve keep the existing read-error outcome — no migration.
