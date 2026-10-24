# Design

## Context

The chat view mounts two sibling surfaces in a flex row: `RightSidebar` (the identity island — avatar, agent name, effective model; docked at xl, hidden below) and `RightPanel` (the tabbed preview column; docked beside the sidebar at xl, overlay sheet below). Opening a file therefore adds a column instead of swapping the bar's content. The preview panel's per-kind renderer registry (`PANEL_RENDERERS`) and the store's `panelTabs` identity-dedup are the seams this change builds on; neither the turn lifecycle nor the live subscription is touched by panel state.

On the data side, delegated tasks already stream everything a trajectory viewer needs: `TaskActivity` carries `task_id`, `transcript_path` (workspace-relative, e.g. `.tasks/<tool-call-id>.output`), `progress`, and `steps`. The transcript file is JSONL, one `{step, content, status}` record per line, with steps `task | text | reasoning | tool | result` (`internal/agents/subagents/transcript.go`). The runtime writes transcripts to `<agent workspace>/.tasks/<id>.output` (`DiskAppendOpener` with `outputBaseDir = surface.agentDir`), the same root the jailed `chat.workspaceFile` reader serves — the two sides meet at one path with no new binding.

The vendored `MarkdownText` reads its text from the assistant-ui message context, so it cannot render a bare string; its component pipeline (memoized component map + remark-gfm + the `.aui-md` class) is reusable directly.

## Goals / Non-Goals

Goals:

- One right bar whose identity view is the empty state of the same surface that shows previews.
- Subagent trajectories viewable in the bar with transcript-fidelity shapes.
- Markdown files renderable in the bar through the chat pipeline.

Non-Goals:

- No persistent lanes (running agents / approvals pinned above tabs) — tabs only; the bar container stays a single tab surface this change.
- No new backend endpoints, no transcript format changes, no migration of old transcripts.
- No new preview surfaces beyond the two above.

## Decisions

- **D1 — One bar, two states, container swap.** `RightSidebar` and `RightPanel` collapse into one `<aside>`: `panelTabs.length === 0` renders the identity view, otherwise the tab strip + active renderer. Collapse (`sidebarCollapsed`) hides the whole bar as today; the chat-header toggle is unchanged. Alternative considered: keeping the sidebar and turning the panel into an overlay over it — rejected; it still hides content behind chrome and keeps two width systems.

- **D2 — One width preference.** The merged bar keeps `buddies.chatSidebarWidth` as its single persisted width, clamped to span both former ranges (min 220, max 560, default 280). The panel's separate `buddies.chatPanelWidth` key is dropped. Below xl the identity view stays hidden (as today) and content renders as the existing full-height overlay sheet with no resize handle.

- **D3 — Trajectory source: the recorded file through the jailed reader.** The tab fetches `chat.workspaceFile(agentID, transcriptPath)` — no new binding, jail guarantees hold. `islandModel` extends `AgentRow` with `transcriptPath` (sourced from the task event the row is already derived from); `RunSurfaces` wires row activation to `openPanelTab({kind: "trajectory", identity: trajectory:<agentID>:<taskID>, …})`. Alternative considered: a Go binding returning parsed steps — rejected; the format is ours, parsing is trivial client-side, and a binding would regen bindings for no jail benefit.

- **D4 — Trajectory rendering maps steps onto existing transcript elements.** Parse the JSONL per line: `task` → user-style message, `text` → markdown (the same standalone markdown body as D6), `reasoning` → the transcript's own reasoning composition (the vendored ghost-variant disclosure with the thread's exact trigger and text classes), `tool` → the transcript's real tool cards, `result` → the child's final answer in a highlighted block. Revised after first smoke (user: tools "not rendered" like the main chat): the flattened `call <name> <args>` / `result <name>: <text>` records are decomposed and PAIRED into one tool-call block per invocation — the call's raw JSON parses into args, and the block renders through the chat's prop-driven `toolRenderers` (the same shaped cards the main transcript mounts, running until the paired result lands; unknown tools render through the card shell plus its fallback body, since the vendored fallback reads root-scope runtime state that does not exist outside the message context). Accepted fidelity loss shrinks to: no per-call timing metadata. Revised again (user, 2026-10-24: the final `result` should not show in the preview): the standalone `result` record is no longer a render block at all — the parent transcript relays the child's answer, so the preview shows only the trajectory; the parser still reads the kind (file truth), `trajectoryBlocks` just drops it.

- **D5 — Live growth by fast polling with streaming affordances.** While the row's task is running, the open tab refetches the file every 500ms and re-parses (records only append, so re-parse is idempotent); polling stops at a terminal status and terminal rows fetch once on open. Revised after first smoke (user: "stream the subagent progress just like the main chat"): the half-second cadence is dressed with the transcript's streaming grammar — a live tail indicator (the thread's shimmering thinking indicator, labeled with the child's latest `TaskActivity.progress` from the store) sits under the stream while the task runs, the last reasoning block carries the live streaming state, and a follow pin keeps the view on the newest blocks unless the reader scrolls away (first stream always follows; afterwards only within ~120px of the bottom). Alternative considered: store-driven streaming of child steps — rejected for now; it needs new event plumbing that the spec doesn't require.

- **D6 — Markdown via a standalone body primitive.** Add `MarkdownBody({text})` under `assistant-ui/elements/`: react-markdown + `memoizeMarkdownComponents` reusing `MarkdownText`'s component map + remark-gfm + the `.aui-md` wrapper, no message context. The file preview tab shows a `Preview | Raw` segmented toggle only when the name ends `.md`/`.markdown`; raw is the default. No new dependencies (react-markdown is already a transitive dep of `@assistant-ui/react-markdown`).

- **D7 — Backend: pin the seam, fix the doc.** Add a Go test asserting `WorkspaceFile` resolves `.tasks/<id>.output` under the agent workspace against the `DiskAppendOpener` write path, and correct `TaskActivity.TranscriptPath`'s stale doc comment (it says `.tasks/task-1.md`); regen bindings (`task common:generate:bindings --force`).

- **D8 — Stale transcripts degrade to the existing error outcome.** Sessions recorded before the trajectory layout (or with pruned `.tasks`) show the read-error state; no backfill, no special-casing.

## Risks / Trade-offs

- [Trajectory file grows large (hundreds of KB)] → the reader returns the whole file per poll; cap the rendered window to the last N steps with a "show earlier" control if needed, and keep the 2s poll only while the tab is visible.
- [Strict-validate misses statement regressions in MODIFIED blocks] → the delta keeps every existing scenario heading byte-stable and changes only what the merge requires; diff the blocks against main specs during apply.
- [Running binary's transcripts resolve nowhere] → expected; D8 makes it a clean error, and a fresh build writes resolvable paths (pinned by D7's test).

## Migration Plan

Pure frontend swap plus test/doc additions; no data migration. Ship behind no flag — the bar's states are behaviorally a subset of today's (identity when empty, previews when open). Rollback is reverting the frontend merge.

## Open Questions

None.
