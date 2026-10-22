# Tasks

## 1. Backend — provider types, key secrecy, verification

- [x] 1.1 Extend `internal/agents` provider type constants with `openrouter`, `openai-compatible`, `anthropic-compatible` plus helpers: known-type check, per-type default base URL, connector mapping (openrouter → OpenAI connector + fixed base; `-compatible` → matching connector + user base). Verify unit tests cover each type's mapping and that unknown types report as unknown
- [x] 1.2 Add `services.ProviderView` (no `APIKey`; `KeySet` bool, `KeyHint` = last 4 runes or empty) and switch all provider reads to it. Verify unit tests assert responses contain no raw key and the hint matches/empties correctly
- [x] 1.3 Change `UpdateProvider` semantics: blank `APIKey` preserves the stored key, non-blank replaces it. Verify unit tests cover both paths plus a name-only edit keeping the key
- [x] 1.4 Reject unknown provider types in `CreateProvider`/`UpdateProvider` with a clear error. Verify unit tests for an unknown type on create and update
- [x] 1.5 Implement `agents.VerifyCredentials(ctx, type, baseURL, key)` — one authenticated GET per type (Anthropic `/v1/models` with `x-api-key`; OpenAI-style `{base}/models` Bearer for openai/openrouter/compatible; Gemini `/v1beta/models?key=`), 10s timeout, 2xx = ok, non-2xx returns truncated endpoint error. Verify unit tests with `httptest` servers: success, auth failure detail, and timeout
- [x] 1.6 Add `SettingsService.VerifyProviderDraft` (type/base URL/key, `provider_id` + blank key falls back to stored key) and `VerifyProvider` (by id). Verify unit tests: draft with typed key, draft fallback to stored key, saved-provider path, and that nothing persists
- [x] 1.7 Extend `ProviderModels`: static catalog for the three canonical types, live OpenAI-style `/models` fetch for `openrouter` and `-compatible` (10s timeout, sorted ids, error → nil). Verify unit tests with a fake endpoint and that canonical types still resolve from the catalog
- [x] 1.8 Run the Go gate: `gofmt -w .`, `go vet ./...`, `go test ./...` all clean

## 2. Bindings & frontend plumbing

- [x] 2.1 Regenerate TypeScript bindings (`wails3 generate`) against the changed service surface. Verify `frontend` typecheck/build compiles against the new `ProviderView` and verify method definitions
- [x] 2.2 Update `lib/client.ts` settings calls to the new shapes (provider view, verify draft/verify saved) and add `settings:open` to `lib/events.ts`. Verify existing frontend tests that stub the client are updated and green

## 3. Frontend — provider catalog, dialog, pane

- [x] 3.1 Create the provider catalog module in `features/settings` (six types: id, label, Base URL placeholder, `requiresBaseUrl`; no decision class). Verify unit tests assert per-type metadata and that the ids match the backend's known set
- [x] 3.2 Build `ProviderFormDialog` per design D7: type select (disabled on edit), type-driven Base URL rules/placeholder, write-only password key field with stored-key placeholder from `KeyHint`, inline "Verify connection" with result line (failure never blocks Save). Verify interaction tests: add flow, edit flow with blank key, required-BaseURL rejection, verify success/failure display
- [x] 3.3 Rebuild `ProvidersPane`: provider cards (type badge, key status from `KeySet`/`KeyHint`, enable toggle, Verify, Edit, Remove), removal confirm with in-use warning (reuse `ConfirmDialog`), models listing as today. Verify updated pane tests cover toggle, verify row outcome, and in-use removal warning
- [x] 3.4 Run `vp check` scoped to `src/` and resolve new violations introduced by this work (pre-existing baseline excluded per repo convention)

## 4. Frontend — settings sidebar layout

- [x] 4.1 Convert `SettingsView` to the two-column layout (design D6): static sidebar config (MODELS → Providers; AUTOMATION → Skills, Tools, Hooks, MCP; icon + label), search input filtering by label, content column with pane title + description + body, mounting only the active pane. Verify tests: initial pane is Providers, switching swaps content, search filters and restores
- [x] 4.2 Update the existing settings tests (tab-strip ids → sidebar ids) and verify both themes render legibly per design.md tokens. Verify `vp check` stays clean for the touched files

## 5. macOS application menu

- [ ] 5.1 Build the application menu in `runDesktop` per design D5 (app submenu: About, Settings… ⌘, below it, Services, Hide, Hide Others, Show All, Quit; then File, Edit, Window; drop Help/View) with the OnClick handler showing the window and emitting `settings:open`. Verify `task build` compiles and the manual GUI checklist passes: About opens the panel, Settings… works from hidden and visible window states, ⌘, fires, ⌘C/⌘V work in text inputs, tray menu unaffected
- [x] 5.2 Subscribe to `settings:open` at the app shell level and navigate to the settings menu via the nav store. Verify a unit test drives the handler and asserts the nav store lands on settings

## 6. Integration

- [x] 6.1 Run the full gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, frontend build, `vp check` scoped to `src/` — all clean
- [ ] 6.2 End-to-end manual pass via `task run`: open settings from the rail and from the menu bar, add/edit/verify/toggle/remove a provider (both themes), confirm keys are never echoed in the UI, and confirm the server build still runs headless (`task build:server`)
