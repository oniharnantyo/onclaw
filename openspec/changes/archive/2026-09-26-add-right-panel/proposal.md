# Proposal: add-right-panel

## Why

The web app renders everything the agent produces inline in the transcript — tool results, documents, screenshots — but there is no surface to *inspect* an artifact at full size. A markdown report the agent wrote, a PDF it produced, or the browser page it is driving all get cramped inline treatment or nothing. The chat pane needs a reusable right panel (docked beside the transcript) that hosts tabbed "sources" — file viewer, browser mirror, channel members — so artifacts are one click away without leaving the conversation.

## What Changes

- **Right panel shell** in the chat surface: a tabbed dock (docked ≥xl, overlay sheet below) with a registry of panel sources; the show/hide toggle sits top-right in the chat header, replacing the agent-configure button in direct agent chats; open/close/badge state lives in a per-chat store slice; closing the last tab closes the panel; switching chats resets tabs.
- **Source registry** mirroring the generative-UI registry pattern: sources register renderers; transcript tool cards register candidate matchers — one tool card feeds both its inline card and a panel "open" affordance. Works identically on live and hydrated transcripts (candidates derive from the transcript tool-card store, never the raw event tap).
- **File source** backed by a new **workspace-files API** that reads the per-agent jail dir (`~/.onclaw/workspaces/<ws>/agents/<agent>/`): `GET …/files?path=` (read) and `mode=list` (one directory level, tree-ready). Renderers: markdown (pre-render pass: frontmatter to a meta chip, relative-URL rewrite against the file's jail dir, then the transcript's ReactMarkdown + shiki + fence-card pipeline), code (shiki), pdf (inline), images, csv (table); xlsx and unknown types get the captioned-degrade download card.
- **Browser mirror source**: mirrors the agent's go-rod browser session per agent+session tab — latest screenshot, URL bar, feed of `browser.*` calls from the transcript. Staleness is labeled ("captured 14:02 · 3 actions since"), never silent; no live CDP bridge.
- **Members source**: the existing channel-members ContextPanel is absorbed into the panel as a registered source (code refactor; it was never a spec requirement).
- **Open policy**: click-to-open from tool-card affordances only; a finished panel-able tool while the panel is closed adds a dot badge on the panel toggle. The agent never pushes the panel open.
- **Serving guards** on the new API: `X-Content-Type-Options: nosniff` everywhere; `text/html` and `image/svg+xml` never served inline (attachment disposition or sandboxed rendering); path resolution confined to the jail root with symlink rejection; workspace scope taken from the auth context, never the URL slug.

## Capabilities

### New Capabilities

- `workspace-files`: authenticated read/list API over an agent's workspace files (jail dir) with path-confinement and content-type-safe serving — the byte lane the panel's file and browser sources fetch through.

### Modified Capabilities

- `web-app/chat`: ADDED requirements — right panel shell (tabs, sources registry, docked/overlay, per-chat lifecycle, badge), tool-card open affordances and badge nudge, file source (pre-render pass + degrade matrix), browser mirror source (per-agent-session tabs, labeled staleness, idle/mirroring/frozen/closed states), members source.

## Impact

- **Backend**: new handler (files read/list) + route registration in `internal/server`; no schema migration; no changes to existing capability serving (`/files/:key` unchanged — its pre-existing inline-HTML exposure is noted in design as follow-up, not fixed here).
- **Frontend**: `web/src/components/chat/` gains the panel shell + source renderers; `ContextPanel` refactored into a source; `livechat`/transcript store untouched (candidates derive from existing tool cards); new panel store slice; markdown pre-render pass reuses the existing fence pipeline.
- **Dependencies**: none new (rendering stack, shiki, rod all present).
- **Design contract**: panel uses existing tokens/motion; responsive degrade reuses the ContextPanel docked/overlay pattern; all four states (loading/empty/error/degrade) rendered per source.
