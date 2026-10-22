# Design

## Context

The settings menu (`frontend/src/features/settings/`) renders five panes behind a daisyUI tab strip; ProvidersPane does CRUD against `SettingsService` (`internal/services/settings_providers.go`) with the raw `store.Provider` (including `APIKey`) bound to the frontend. The Go runtime knows three provider types (`internal/agents/catalog.go`: anthropic, openai, gemini) with a static model catalog and `CatalogModels` returning nil for unknown types. `main.go` sets no application menu, so Wails installs its default (`AppMenu/File/Edit/View/Window/Help`); Wails β.29 offers `Menu.AddSubmenu`, menu roles (`About`, `ServicesMenu`, `Hide`, `HideOthers`, `UnHide`, `Quit`, `EditMenu`, …), `SetAccelerator`, and `App.Menu.Set`. The frontend already has a Go→frontend event path (`transport.ts` wrapping `Events.On`, used by `gateway:state`). design.md (the design system) is the styling law: daisyUI semantic classes, light + dark, chrome → surface → glass-panel grammar. See proposal.md for motivation and specs/ for the behavior contract.

## Goals / Non-Goals

**Goals:**

- Settings navigation that scales: sidebar + content pane, per the reference layout (Kiro-style two-column settings).
- Provider management at onclaw-v2 quality, LLM-only: six types, type-driven form, write-only keys, verification.
- Keys stop crossing the service boundary in plaintext.
- Standard macOS settings entry point (menu bar + ⌘,).

**Non-Goals:**

- Decision-class providers, catalog mapping, new panes, secrets encryption at rest (see proposal Out of scope).
- Windows/Linux menu QA (accelerator string is portable; only darwin is exercised).
- Reworking the Skills/Tools/Hooks/MCP pane bodies — only their container changes.

## Decisions

### D1: Provider type catalog — presentation in frontend, membership in Go

The six type ids are the contract. Go gets a validation set (`agents.Provider*` constants extended with `openrouter`, `openai-compatible`, `anthropic-compatible`); `CreateProvider`/`UpdateProvider` reject unknown types with a clear error. The frontend owns a catalog module (per-type label, Base URL placeholder, `requiresBaseUrl`) mirroring onclaw-v2's `PROVIDER_CATALOG_TYPES` minus `typesafe`. Serving the catalog from a bound Go service was considered and rejected — static display data does not justify a service surface; a mismatch would surface in tests.

### D2: Key secrecy — view DTO for reads, blank-means-keep for writes

New `services.ProviderView` (ID, Type, Name, BaseURL, Enabled, KeySet bool, KeyHint string, CreatedAt, UpdatedAt) returned by all reads; Wails bindings regenerate so the webview never sees `APIKey`. `KeyHint` = last 4 runes of the key, empty when unset. Writes keep `store.Provider` as input with one rule change: `UpdateProvider` with `APIKey == ""` preserves the stored key (create still requires the key for key-requiring types — enforcement is UI-level; the service accepts a keyless provider since compatible endpoints may legitimately need none, matching onclaw-v2's keyless-declaration semantics minus the checkbox: an empty key on a key-requiring type is allowed at the service layer and flagged by verification). Alternative considered — field-level JSON marshaling tricks on `store.Provider` — rejected: the store model is the persistence type, mixing view concerns into it leaks again with the next binding.

### D3: Verification — plain HTTP probes in `internal/agents`, not eino calls

`agents.VerifyCredentials(ctx, type, baseURL, key) (ok bool, detail string, err error)`: one cheap authenticated GET per type — Anthropic `GET /v1/models` (`x-api-key` + `anthropic-version`), OpenAI-style `GET {base}/models` with Bearer (openai, openrouter fixed base, both `-compatible` with user base), Gemini `GET /v1beta/models?key=`. 10s timeout; any 2xx = ok; non-2xx returns the endpoint's error message truncated. `SettingsService.VerifyProviderDraft` (unsaved values; `provider_id` + blank key falls back to stored key) and `VerifyProvider` (saved id) wrap it. Using eino model components for a 1-token completion was considered and rejected: heavier, non-uniform across types, costs money. base URL defaults per type when blank (canonical origins).

### D4: Model listing — live fetch for openrouter + compatible types

`ProviderModels` keeps the static catalog for the three canonical types; for `openrouter` and `-compatible` it live-fetches the OpenAI-style `/models` list (10s timeout, ids sorted, error → empty list which the UI already renders as "No models available"). Caching considered and deferred — the settings surface is infrequently used.

### D5: Application menu — rebuild the app submenu, keep Edit/File/Window, drop Help

In `runDesktop`: `menu := wailsApp.NewMenu()`; `appSubmenu := menu.AddSubmenu(services.AppName)`; `AddRole(About)`; `Add("Settings…").SetAccelerator("cmdorctrl+,").OnClick(...)` — the handler calls `window.Show()` and `wailsApp.Event.Emit("settings:open")`; then separator, `AddRole(ServicesMenu)`, separator, `AddRole(Hide)`, `AddRole(HideOthers)`, `AddRole(UnHide)`, separator, `AddRole(Quit)`. Menu bar then adds `FileMenu` (Close Window → existing hook hides), `EditMenu` (clipboard/undo in inputs), `WindowMenu`; `wailsApp.Menu.Set(menu)`. `HelpMenu` is dropped — its only item is a wails.io link (template debris). ViewMenu dropped too (reload/devtools remain reachable in dev builds via URL). Emitting an untyped string event needs no `RegisterEvent` (only typed payloads do). Frontend: `settings:open` constant in `lib/events.ts`, subscribed at the app shell level (beside the gateway-state listener), handler = `useNavStore.getState().setMenu("settings")` — navigation state already lives in the nav store.

### D6: Settings layout — static sidebar config, local search state

`SettingsView` becomes a two-column layout: sidebar (search input + grouped nav: MODELS → Providers; AUTOMATION → Skills, Tools, Hooks, MCP) and a content column (pane title, one-line description, body). Sidebar entries are a static config array (`key`, `label`, lucide `icon`, `group`, `description`) in `features/settings`; search is local `useState` filtering labels. Panes mount only the active one (replace the current render-all-hidden pattern — cheaper and simpler). Styling per design system: sidebar is chrome-level (`glass-chrome`/`bg-base-200` semantics), content is `glass-surface`; active entry uses the existing nav-active idiom from `App.tsx`. Test ids preserved where behavior is unchanged (`settings-view`); new ids for nav items and search; old `settings-tab-*` ids removed with their tests updated.

### D7: ProvidersPane — extract ProviderFormDialog, reuse shared primitives

Mirror onclaw-v2's split: `ProviderFormDialog` (type select with per-type placeholder/required rules, name, base URL, write-only password key field with stored-key placeholder from `KeyHint`, inline Verify button + result line) and the pane (add button, provider cards with type badge + key status + enable toggle + Verify/Edit/Remove, ConfirmDialog reuse for removal with the in-use warning). All styling daisyUI semantic classes per design.md; the existing `.btn-icon-glass` idiom applies to icon-only actions with `aria-label`s. Models listing button stays as today (`ProviderModels`).

## Risks / Trade-offs

- [Archive ordering: these deltas modify capabilities that only exist in the un-archived `add-base-gateway-app` change] → documented in proposal.md; archive `add-base-gateway-app` first (it is 44/48). `openspec validate` already confirms validity and only notes the ordering.
- [Blank-key-means-keep could silently wipe keys on ordinary edits if mishandled] → service-level unit test: update with empty key preserves the stored secret; update with non-empty key replaces it.
- [Compatible-endpoint verification/model listing can hang or return non-standard shapes] → 10s client timeout; non-2xx/parse failures surface as failure detail or "No models available" rather than errors that block the UI.
- [Menu role assembly is native-glue and not unit-testable] → manual GUI checklist task (`task run`): About opens the panel, Settings… works from both hidden and visible states, ⌘, fires, clipboard shortcuts survive in text inputs.
- [Live model listing for openrouter needs network at settings time] → failure degrades to "No models available"; acceptable for now, caching deferred.
- [ProviderView rename ripples through bindings + client.ts + pane in one step] → regenerate bindings before touching frontend; `vp check` scoped to `src/` as the gate.

## Migration Plan

No data migration: the store schema (including the `APIKey` column) is unchanged; secrecy is a read-path change. Deploy = rebuild (bindings → frontend → binaries). Rollback = revert; no persisted-state cleanup needed. Archive-order dependency as above.

## Open Questions

- Whether openrouter gets a static fallback model list for offline use — deferrable; live listing is the default behavior.
