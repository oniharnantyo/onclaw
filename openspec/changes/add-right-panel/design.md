# Design: add-right-panel

## Context

The chat surface renders agent output inline only. The pieces a panel needs exist: markdown pipeline (ReactMarkdown + shiki + fence-card registry), go-rod browser tools whose screenshots land in the agent jail dir, capability-URL serving for published blobs, and a hardcoded right aside (ContextPanel) for channel members. Two gaps: no panel shell, and no web-reachable lane to the jail dir — everything the agent writes (`files.write`, `browser.screenshot`) is unreachable; only explicitly published blobs serve.

Physical layout, already fixed by `domain.AgentWorkspaceDir`: jail root is `~/.onclaw/workspaces/<ws-slug>/agents/<agent-slug>/`. `livechat.ts` folds both the live tap and history replay into transcript tool cards (`agent.tools[]` — `{callId, name, args, res, ms, error}`), deduping the overlap; the generative-UI registry (`registerSpecRenderer`/`registerToolRenderer`) is the established frontend registry pattern.

Constraints from AGENTS.md that shape this design: granular dependency injection for the new handler (workspace root + stores it actually uses, never an aggregate); registries over core edits (panel sources register, nothing edits the transcript renderer); design-contract tokens/motion/responsive rules.

## Goals / Non-Goals

Goals:
- One reusable panel shell + source registry; members become a source, file and browser sources register the same way.
- A secure, workspace-scoped byte lane to the jail dir (read + one-level list).
- Markdown that renders *properly* — identical to transcript rendering, with frontmatter and relative assets handled.
- A browser mirror that is honest about staleness.

Non-Goals:
- File-tree browsing UI (the list API is shaped for it; the UI is a later change).
- Live CDP bridge to the agent's browser; auto-capture on navigate (v2 knob if the mirror proves out).
- Inline xlsx renderer (degrade + download for now).
- Agent-pushed panel opening (the fence lane already covers agent-expression inline).
- Fixing the pre-existing inline-HTML exposure on `/files/:key` (noted below; separate hardening).
- Panel persistence across reload, resizable divider, keyboard shortcuts.

## Decisions

### D1 — Panel shell is a per-chat store slice with strict open policy
Panel state (`open`, `tabs[]`, `activeId`, `badge`) lives in a new store slice scoped to the current chat: switching chats resets tabs; opening an artifact with an existing tab focuses it (dedup key = kind + payload identity); closing the last tab closes the panel. Docked ≥xl / overlay sheet <xl reuses the exact pattern ContextPanel already established in `ChatRoute`. The panel never opens from a tool event — badge only.

Header placement (user decision, 2026-09-22): the toggle takes the chat header's top-right slot. In direct agent chats it *replaces* the configure button (`btn-configure-agent`, sliders) — the configure action leaves the chat header entirely and stays reachable from the Agents screen. Channels keep the members avatar stack, which becomes a members-tab opener per D7.

*Alternative considered*: an app-shell-level global dock surviving chat switches — rejected: panel contents are run artifacts and runs belong to chats; a global dock shows artifacts with no transcript context. Revisit if Runs/Agents screens want the panel (the shell component is written route-agnostic so it can lift later).

### D2 — Sources and candidates are registries; candidates derive from transcript cards, not events
Two registration surfaces, mirroring the generative-UI registry: `registerPanelSource(kind, renderer)` and `registerPanelCandidate(matcher)` where a matcher maps a transcript tool card to `{kind, title, payload}` (e.g. name startsWith `browser.` → browser tab for that agent+session; result carries a published URL → file tab). Extractors read `agent.tools[]` — the same folded data both live and hydrated paths produce — so old conversations get identical affordances. This deliberately avoids the live≠hydrated drift class that bit the timeline fold and compaction dividers.

*Alternative considered*: deriving candidates from raw `tool_call_finished` events in the live tap — rejected: hydrated transcripts would lose affordances.

### D3 — The byte lane is a new workspace-files API over the jail dir
`GET /api/v1/workspaces/:ws/agents/:agent/files?path=…` (read) and `…&mode=list` (one directory level: name, kind, size, modified — tree-ready without recursion). The handler takes the workspace root + the stores it needs as positional deps (composition-root resolved, no nil guards). Workspace scope resolves from the auth context; the URL's slugs locate, never authorize.

*Alternatives considered*: capability-URL-only (zero backend but only published blobs — `files.write` markdown and screenshots stay unreachable); mint-at-source (each tool publishes like document.create — opt-in per tool, blind to everything already on disk, and pollutes tool results). The jail API is the only lane that covers everything retroactively; capability URLs remain the public/cross-context lane.

### D4 — Serving guards are the API's requirements, not hardening afterthoughts
On every response: `X-Content-Type-Options: nosniff`; `text/html` and `image/svg+xml` carry attachment disposition (stored content cannot execute on the app origin — an authenticated member's agent can write such files *deliberately*, unlike capability keys which are at least unguessable); path resolution does `Clean` + root-prefix check + symlink rejection (`Lstat`) at the boundary; traversal/absolute/symlink-escape all return not found (no existence oracle outside the root). Guard tests are acceptance criteria, not extras.

### D5 — Markdown renders through a pre-render pass into the transcript pipeline
The file source fetches text, then: strip YAML frontmatter into a meta chip; rewrite relative `src`/`href` references against the file's own jail directory through the files API (transcript fences already reject relative URLs — without the rewrite every asset 404s against the app origin); render the body with the same ReactMarkdown/shiki/fence-card stack AgentMessage uses, so mermaid, chart fences, and code blocks are identical by construction. No separate markdown renderer is maintained.

### D6 — Browser mirror is per agent+session, labeled-stale, transcript-fed
Tab payload is `{agentSlug, sessionId}` (sessions are already keyed `{sessionID, agentDir}` — multi-agent channels get independent tabs). Screenshot, address bar, and call feed all come from transcript tool cards. The screenshot caption states capture time and actions-since count — staleness is visible, never silent. States: idle → mirroring → frozen (run finished, last screenshot retained) / closed (session torn down). No polling loop, no CDP.

*Alternative considered*: backend auto-capture on `navigate` for a near-live mirror — deferred: it adds rod captures and jail writes to every tool design; ship labeled staleness first, promote if the mirror earns daily use.

### D7 — Members become a source; the special case dies
ContextPanel's body moves into a registered `members` source; `pos.showContext` channel gating becomes "members tab offered in channels only". Behavior (list, presence, add/remove eligibility) is unchanged — this is the refactor that proves the registry is real.

## Risks / Trade-offs

- [Jail file deleted between tab open and fetch] → explicit not-found state per spec; tabs are views, not handles.
- [Very large markdown/code files block the pane] → render without virtualization in v1 but degrade: above a size threshold show head + download card (same captioned-degrade pattern).
- [Files-API guard regression = filesystem escape or stored XSS] → guard set is spec-level with rejection scenarios; traversal/symlink/content-type tests are acceptance criteria in tasks.
- [Mirror identity across runs] → new run = new session id = new tab; a frozen old tab stays honest via its frozen state.
- [`/files/:key` inline-HTML exposure remains] → acknowledged pre-existing gap; this change's guards set the pattern; hardening the legacy route is a separate small change.
- [Panel on 360px viewports] → overlay sheet is the existing ContextPanel pattern already proven at small widths; no new responsive surface.

## Migration Plan

Three independently shippable slices, no schema migration, no feature flags:
1. Shell + registries + members source (pure refactor; ContextPanel special case removed in the same slice so there is no dual-panel window).
2. workspace-files API + guards + file source (backend then frontend; API is additive).
3. Browser mirror (frontend-only, rides slice 2 for screenshot bytes).

Rollback is revert-per-slice; nothing writes to the database and no existing route changes.

## Open Questions

None blocking. Panel width (fixed ~400px v1), xlsx renderer, and auto-capture-on-navigate are revisited after live use.
