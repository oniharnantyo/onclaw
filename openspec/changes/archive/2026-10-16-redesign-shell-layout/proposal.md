# Proposal: redesign-shell-layout

## Why

The shell preview convergence (2026-10-16, viz previews + captured screenshots)
settled the app's window layout: an Orbit-style title-only strip, a collapsible
icon rail, chat history in its own Sessions island, and Settings opening as a
full island. Today the desktop window keeps a deliberately bare strip, the rail
is fixed-width, session switching lives inside the chat surface, and settings
replaces only part of the layout — the shell reads as a chat page, not a
workbench.

## What Changes

- **Title-only strip:** the window strip shows the active view's name and a
  context line as plain text (e.g. "Chat / 3 sessions · 2 running", "Settings /
  Providers · 2 configured"). No buttons, no island box — the strip holds only
  text, so the stock 50px invisible-titlebar drag band stays untouched
  (frontend-only; no `main.go` change).
- **Collapsible navigation rail:** the Chat/Agents rail gains a panel toggle in
  its header that collapses it to icon-only width and back.
- **Sessions island (chat):** chat history moves out of the chat surface into
  its own glass island column — header with a new-session button, a search
  pill that filters sessions, and session entries (title + relative time/model
  meta) with the active session in the primary tint. Hidden while Settings is
  open.
- **Settings as a full island:** opening Settings (rail, ⌘, or menu) replaces
  the whole work area as one island — pane sidebar + pane content; the chat
  toolbar row and Sessions island do not render beside it.
- **Pane header action row:** each settings pane's title row carries its
  primary action at the top-right (Providers → "Add provider").
- **Provider row identity:** provider rows lead with a brand logo tile
  (Anthropic, OpenAI; initial-glyph fallback for providers without a known
  mark), then name, mono model/credential line, and verification status.

Deliberately NOT changed: the 50px drag band height and `main.go` window
options (no controls live in the strip), the agents feature layout, the
settings providers port itself (`refactor-settings-providers` — this change
only adds visual identity to its rows), and the RightSidebar.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `app-shell`: title-only window strip, collapsible navigation rail, and
  Settings opening as a full-island view (existing deltas: background
  lifecycle, tray, application menu — unchanged).
- `chat-view`: the session layer becomes a Sessions island column with search
  (modifies "Sessions organized per agent"; transcript, right sidebar,
  composer untouched).
- `settings`: pane header action row and provider row identity (panes,
  secrecy, and verification requirements from in-flight changes unchanged).

## Impact

- `frontend/src/App.tsx` — strip title (view-driven), rail collapse state.
- `frontend/src/features/chat/` — SessionRail becomes the Sessions island
  (search state added); `ChatView` layout.
- `frontend/src/features/settings/` — pane header action row slot;
  `ProvidersPane` rows gain logo tiles.
- `frontend/src/components/` — possibly a shared `PaneHeader` primitive;
  `avatar.ts`-style lib for brand marks.
- No API, store-schema, or bridge changes. Rail collapse preference persists
  client-side (localStorage), not in the settings store. Both daisyUI themes
  must stay legible; mobile (<lg) layout keeps the existing bar pattern.
