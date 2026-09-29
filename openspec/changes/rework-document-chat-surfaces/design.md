# Design

## Context

`openPanelTab` writes the Zustand panel slice; `RightPanel` (the only reader/renderer) mounts exclusively in `ChatRoute`. DocumentsPane, AgentConfigModal, Composer, and the panel's own `DocumentsListSource` all mint document tabs — the first two from surfaces with no panel host. `DocumentSource` is self-contained (derives its workspace from the store, resolves capability URLs by name when absent), so it renders fine outside the chat route. The panel's `documents` source is registered but unreachable: no caller mints `kind: 'documents'`.

## Goals / Non-Goals

Goals: preview works on every surface it's offered; the panel Documents listing becomes the single chat-side documents surface (per user direction, replacing the floating popover); insert-as-mention survives the move.

Non-Goals: no global `RightPanel` mount (conflicts with D1 chat-scoped panel semantics); no changes to citation chips, tool cards, backend, or the visibility lens.

## Decisions

- **D1 — Standalone preview modal on panel-less surfaces.** `DocumentsPane` and `AgentConfigModal` render `DocumentSource` inside the existing `Modal` component, keyed by `{name, url}`; the modal opens via local state, replacing the `openPanelTab` call. *Alternative:* mount `RightPanel` globally — rejected: panel tabs are chat-run artifacts (D1 of add-right-panel); settings pages would inherit chat state.
- **D2 — Composer button opens the panel tab.** The `btn-documents-popover` toolbar button (same position/label) calls `openPanelTab({kind:'documents', payload:{}})`; popover state (`docsOpen/docsPhase/docs`) is deleted. One fetch per open still holds — `DocumentsListSource` fetches on mount with the conversation lens. **AMENDED 2026-09-28 (user pivot, live pass feedback):** the affordance moved from the composer toolbar to the **chat header**, beside the panel toggle (`btn-panel-documents`). Closing the Documents tab from the panel strip had left the surface unreachable except through the small composer button; the header toggle makes documents a first-class panel view — pressed while the panel shows the Documents listing (the tab fills the panel), click closes the tab (last-tab-closed closes the panel) or mints it back. The composer keeps only the mention bridge; the lens gate moves with the affordance.
- **D3 — Mention bridge via ChatRoute context.** `ChatRoute` already passes `ctx` into `RightPanel` (carries `openPanelTab`). It gains `insertDocumentMention(doc)`; the composer registers its insertion callback (text token + `docChips` state) with ChatRoute via a ref-style registration prop threaded through `ChatView`. The panel row action calls it and closes/keeps the panel open (keep open — matches preview-then-send flow).
- **D4 — Panel row keeps preview as primary click; Insert is an explicit secondary action** (stopPropagation), visually a small text button per row.

## Risks / Trade-offs

- [Mention insertion loses one-click proximity of the popover] → same toolbar button, same count of clicks to insert (button → Insert); the listing gains descriptions, scope badges, and index status.
- [DocumentSource expects a `tab` shape] → wrap as `{kind:'document', title:name, payload:{name,url}}`; identity by name+url matches chat citation dedup semantics.

## Migration Plan

Component-level change; no data or API migration. Ship behind existing tests plus new ones.

## Open Questions

- None.
