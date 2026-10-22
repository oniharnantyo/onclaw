# Tasks

## 1. Storage: provider model rows

- [x] 1.1 Add `ProviderModel` type and `ProviderModelStore` interface (`internal/store/types.go`, `providermodels.go`) with Create, ByID, ByProvider, Update, Delete, DeleteByProvider, UpsertMissing; verify `go build ./...`
- [x] 1.2 Implement the sqlite store (CRUD, `UNIQUE(provider_id, model_id)` duplicate rejection, FK cascade on provider delete) with tests covering duplicate-id rejection and provider-delete cascade; verify `go test ./internal/store/...`
- [x] 1.3 Extend the fake store with the same behavior and verify existing store-consumer tests still pass (`go test ./...`)
- [x] 1.4 Add migration 000010 creating `provider_models` and update the schema-version tests' pinned migration count; verify `go test ./internal/store/sqlite/...`

## 2. Capability resolver and seeding

- [x] 2.1 Add the capability resolver in `internal/agents` (stored row → static catalog → defaults) with table tests over all three tiers and the disabled-row exclusion; verify `go test ./internal/agents/ -run Capability`
- [x] 2.2 Route context accounting, effort validation, and attachment image/PDF degradation through the resolver; verify `go test ./internal/agents/...`
- [x] 2.3 Implement the idempotent startup seed sweep (canonical providers get catalog rows they lack; live-listed providers untouched) with a test that re-running preserves user edits and toggles; verify `go test ./internal/services/...`
- [x] 2.4 Seed canonical providers on provider create in the settings service; verify via the service test for a new `anthropic` provider

## 3. Provider model service surface

- [x] 3.1 Rewrite `ProviderModels` as a store read returning enabled rows with capability data; verify `go test ./internal/services/ -run ProviderModels`
- [x] 3.2 Add model CRUD service methods (add with duplicate rejection, edit, delete, toggle) with secrecy unchanged (no key exposure in any DTO); verify service tests for each operation
- [x] 3.3 Implement refresh: `ListModels` upsert for live-listed types, catalog re-seed for canonical; failed fetch leaves the list untouched (test with a failing fetch); verify `go test ./internal/services/ -run Refresh`
- [x] 3.4 Reject model selection not in the provider's enabled stored rows at turn/override time with a clear per-turn error (replacing the free-form allowance); verify `go test ./internal/agents/ -run 'Model|Effort'`
- [x] 3.5 Run `gofmt -w .`, `go vet ./...`, `go test ./...`; verify all green

## 4. Settings shell in the island

- [x] 4.1 Remove the `inSettings` shell branch: SettingsView mounts inside the content island, Back header and settings strip row deleted, strip overlay gains the Settings title, island columns clear the drag band; verify `vp check src` and manual GUI pass at both window widths
- [x] 4.2 Update app-shell frontend tests (full-island expectations → in-island: rail visible, chat layer absent, chat restores on return); verify `npm test` in `frontend/`

## 5. Providers master-detail and model list UI

- [x] 5.1 Split ProvidersPane into list column (brand tile, name, status dot, Providers/Custom providers groups) + detail column with selection state and add-selected create form; verify pane tests for selection and grouping
- [x] 5.2 Build the detail view: name/type/Base URL/key draft with Save (blank-key rule), header toggle, verify (draft + saved), two-step delete with in-use confirm; retire `ProviderFormDialog` porting its type-catalog field logic; verify pane tests cover edit-save, verify outcomes, and both delete paths
- [x] 5.3 Build the Model list section: rows (mono id, human-readable context badge, Vision badge, edit, delete, toggle), add-model form, refresh button with failure reporting; verify pane tests for add/edit/toggle/delete/refresh-failure
- [x] 5.4 Verify credential-secrecy UI: reveal unmasks typed input only, key hint line per spec; verify `vp check src` and pane tests
- [x] 5.5 Verify both themes across the new pane surfaces; verify manual GUI pass in light and dark

## 6. Agents master-detail

- [x] 6.1 Re-home `AgentConfigModal` content into `AgentConfigPane` (same tabs, props, save/delete behavior) mounted in the Agents menu's detail column; delete the modal shell and its custom-id model option; verify existing editor tests pass re-targeted to the pane
- [x] 6.2 Build the agent list column (avatar, name, model line, prompt status with retry, chat affordance) with first-agent auto-select and empty state; update agent-management tests (grid wording → list, selection shows editor, retry on entry); verify `npm test`
- [x] 6.3 Wire list refresh on save/delete so entries reflect configuration changes without remount; verify `npm test` for the upsert paths

## 7. Bindings and integration

- [x] 7.1 Run `task build` and regenerate Wails bindings (frontend tsc must see the new DTOs); verify `task build` completes and `vp check src` passes
- [x] 7.2 Full gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `npm test` in `frontend/`, `vp check src`; verify all green
- [x] 7.3 Server-mode smoke: `bin/buddies-server` serves the settings UI with the rail visible, provider model CRUD works over HTTP, and an agent turn on a stored custom model resolves its stored context window; verify via the live-bridge recipe
- [x] 7.4 Migration smoke on a copy of a pre-change database (existing canonical provider gains seeded rows; live-listed provider starts empty); verify schema and row counts
