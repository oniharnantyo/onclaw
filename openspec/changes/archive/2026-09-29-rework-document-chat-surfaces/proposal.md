# Proposal

## Why

Three live-verified defects (2026-09-28) in the reference-document surfaces: (1) the Documents settings pane's preview (eye) button and the agent-config modal's preview links call `openPanelTab`, but the right panel mounts only on the chat route — the clicks update invisible store state and nothing renders; (2) the right panel's Documents listing source (task 10.5) is registered but unreachable — no affordance anywhere mints a `documents` tab; (3) the user asks for the floating composer documents popover to live in the right sidebar instead, which doubles as the missing panel entry point.

## What Changes

- Panel-less surfaces (Documents settings pane, agent-config modal documents section) render the document preview in a standalone modal hosting the existing `DocumentSource` component, instead of the unreachable right-panel call.
- The composer's documents popover is removed; its toolbar button now opens the right panel's Documents listing tab (same visibility lens, dedup via `openPanelTab`).
- The panel Documents listing gains an insert-as-mention row action (moved from the popover) bridged through the mounting screen's context; click still opens the in-panel preview.
- No change to citation chips (10.2), tool cards, or the preview source itself.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `workspace-reference-documents`: the "Chat surfaces" requirement changes — the composer affordance opens the right panel's Documents listing (replacing the floating popover), the panel listing carries the insert-as-mention action, and panel-less surfaces (workspace settings, agent config) present the document preview in a standalone modal.

## Impact

- `web/src/screens/settings/DocumentsPane.tsx` — preview modal instead of `openPanelTab`.
- `web/src/modals/AgentConfigModal.tsx` — same treatment for its preview links.
- `web/src/components/chat/Composer.tsx` — popover removed; button opens the panel documents tab.
- `web/src/components/chat/panel/sources/documents/DocumentsListSource.tsx` — insert-as-mention row action.
- `web/src/screens/ChatRoute.tsx` — context bridge for the mention-insert callback.
- Component tests updated; no API or backend change.
