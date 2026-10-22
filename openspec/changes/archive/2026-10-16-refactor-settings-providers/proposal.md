# Proposal

## Why

The settings menu's Providers pane is a functional stub: only two provider types, the raw API key round-tripped to the webview, no connection verification, and a generic form. The predecessor project onclaw-v2 already built this surface properly — port its implementation, scoped to LLM providers. Two navigation gaps compound it: the settings menu is a cramped tab strip that will not scale as panes are added, and on macOS there is no standard way to reach settings (no "Settings…" item in the application menu below "About buddies").

## What Changes

- **Settings layout**: replace the tab strip with a two-column settings view — a left settings-nav sidebar (pane search, items grouped into MODELS and AUTOMATION sections, icon per pane) and a right content pane (pane title, description, body). Houses the existing five panes: Providers (MODELS); Skills, Tools, Hooks, MCP (AUTOMATION). No placeholder sections.
- **Providers pane rebuilt** on the onclaw-v2 pattern, LLM-only (no decision class). Six provider types in a catalog: `openai`, `anthropic`, `gemini`, `openrouter`, `openai-compatible`, `anthropic-compatible` — each with display label, Base URL placeholder, and whether Base URL is required. Type select is disabled while editing. Inline enable/disable toggle per provider. Remove keeps the confirm dialog with the "still used by agents" warning.
- **Key secrecy** **BREAKING** (service API change): provider listings and reads no longer return the raw API key. The frontend-facing shape gains `KeySet` (bool) and `KeyHint` (last 4 characters). Updating a provider with a blank key keeps the stored key.
- **Connection verification**: new Go capability. The provider dialog's "Verify connection" tests the entered values (type, base URL, key — falling back to the stored key when editing and left blank) without saving; saved providers can be verified from the list. Implementation is a cheap authenticated call per type (list-models style).
- **Provider types on the backend**: `openrouter` maps onto the OpenAI connector with a fixed base URL; the two `-compatible` types use the matching connector with the user-supplied base URL. Model listing (`ProviderModels`) works for all six types.
- **macOS application menu**: the desktop host installs a custom application menu whose app submenu places "Settings…" (accelerator ⌘,) directly below "About buddies". Activating it shows the window (if hidden) and navigates the frontend to settings via a `settings:open` event. Desktop-only; server/web host untouched.

**Out of scope** (follow-ups): decision-class providers (onclaw-v2's routing backends); catalog mapping for community model catalogs; new settings panes beyond the existing five; sidebar search beyond filtering the pane list; Windows/Linux menu polish (accelerator is `cmdorctrl+,` so it ports, but only macOS is exercised); secrets encryption at rest.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

> Note: `openspec/specs/` is currently empty — the base capabilities below are introduced by the in-flight `add-base-gateway-app` change, which must be archived first for these deltas to merge cleanly.

- `settings`: the Providers-pane requirement is rewritten (type catalog, write-only key with `KeySet`/`KeyHint`, enable toggle, connection verification); a new requirement specifies the two-column settings navigation layout replacing the tab strip. The Skills/Tools/Hooks/MCP pane requirements are unchanged — the sidebar re-houses them without behavior change.
- `app-shell`: a new requirement adds the macOS application menu with "Settings…" below "About buddies", its show-window + navigate behavior, and its desktop-only scope.

## Impact

- **Go**:
  - `internal/services/settings_providers.go`: frontend-facing provider DTO (drop raw `APIKey`, add `KeySet`/`KeyHint`), blank-key-means-keep update semantics, `VerifyProviderDraft` / `VerifyProvider` methods.
  - `internal/agents`: provider type constants + connector mapping for the six types (openrouter → OpenAI connector + fixed base URL; `-compatible` → matching connector + user base URL); model listing per type.
  - `main.go` (`//go:build !server`): application menu construction (`Menu.AddSubmenu` + About role + custom Settings… item with accelerator), `settings:open` event emission.
- **Frontend**:
  - `frontend/src/features/settings/`: layout refactor (sidebar + content), rebuilt `ProvidersPane` + provider form dialog, type catalog module; updated tests.
  - `frontend/src/lib/events.ts`: `settings:open` event name; subscription beside the gateway event listener.
  - `frontend/src/lib/client.ts`: provider DTO shape, verify calls.
- **Bindings**: regenerate `frontend/src/bindings` (`wails3 generate`) after the service change.
- **Design system**: sidebar, provider cards, and dialog follow design.md (daisyUI semantic classes, both themes, chrome → surface → glass grammar).
