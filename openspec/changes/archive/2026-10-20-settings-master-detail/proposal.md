# Proposal

## Why

Settings currently takes over the whole window (the navigation rail disappears behind a full-bleed page with a Back header), providers are a flat card list whose model availability is *computed* (static catalog or a live listing that fails to empty), and agents are a card grid whose configuration hides behind a modal. The reference UX keeps the left navigation visible everywhere, manages each provider's model list as stored, editable data (the donor's "Model settings" page), and edits agents in a master-detail layout. Beyond parity, computed model availability is a real defect: a compatible provider whose `/models` fetch fails offers zero models, and there is no way to add a model the catalog or listing does not know.

## What Changes

- Settings renders **inside the content island** like chat and agents: the navigation rail stays visible, the settings Back header retires, and the settings pane nav becomes the island's left column. **BREAKING** for the `app-shell` full-island requirement, which is replaced.
- The **Providers pane becomes master-detail**: provider list on the left (brand tile, name, status dot), selected provider's configuration on the right (name header with toggle, Base URL, type-as-API-format, key field with reveal), edited in place instead of through the edit dialog.
- **Per-provider model lists become stored data**: a new `provider_models` table (migration) seeded from the built-in catalog for canonical types; add / edit / delete / enable-toggle per model with context-window and Vision badges, plus a refresh affordance that re-pulls a compatible provider's live listing (or re-seeds canonical) without discarding custom rows or toggles. **BREAKING**: `ProviderModels` stops computing availability on the fly — agent-config and composer pickers serve the provider's stored enabled models.
- **Runtime capability resolution goes store-first**: context-window accounting, effort options, and image/PDF input degradation resolve from the provider's stored model rows, falling back to the static catalog, then defaults.
- The **Agents menu becomes master-detail**: agent list column on the left (avatar, name, prompt status with retry), the existing five-tab configuration editor re-homed from `AgentConfigModal` into the right detail column; the create wizard stays a modal. No data-model change.

## Capabilities

### New Capabilities

(none — provider model storage lands inside the existing `settings` and `data-store` capabilities)

### Modified Capabilities

- `app-shell`: replaces "Settings opens as a full-island view" with in-island settings rendering; the strip requirement's Settings mention now describes an overlay title like the agents view's.
- `settings`: Providers pane reworked to master-detail with stored model lists (new authoring/refresh requirement); provider row identity becomes the list entry; verification moves to the detail view; credential secrecy gains the reveal-toggle clarification.
- `agent-management`: card grid replaced by a master-detail layout (list + detail-column configuration editor); create/delete wording follows the list.
- `data-store`: new requirement for stored per-provider model rows.

## Impact

- **Go**: migration 000010 (`provider_models` + canonical backfill); `internal/store` (new store interface + sqlite + fake); `internal/services/settings_providers.go` (model CRUD + refresh; `ProviderModels` reads the store); `internal/agents/catalog.go` consumers get a provider-scoped capability resolver (turn-loop accounting, attachment degradation, effort validation); Wails bindings regenerate.
- **Frontend**: `App.tsx` settings branch collapses into the standard island; `SettingsView` loses the Back header; `ProvidersPane` splits into list + detail; `AgentConfigModal` content re-homes as a detail pane inside `AgentsView` (largest single move — component content stays intact).
- **Security posture unchanged**: the stored key still never returns to the frontend; the reveal toggle only unmasks the session's typed input.
- **Risk center**: the capability resolver — a wrong fallback makes the ContextMeter and attachment degradation silently lie for custom models.
