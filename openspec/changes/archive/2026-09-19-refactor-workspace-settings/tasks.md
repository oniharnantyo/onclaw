## 1. Schema and stores

- [x] 1.1 Migration 000058: add nullable `default_provider_id` (composite FK `(workspace_id, default_provider_id) → workspace_providers(workspace_id, id)`) and `default_model` to `workspaces`; make `agents.provider_id`/`agents.model` nullable; write down migrations
- [x] 1.2 Extend domain workspaces entity + agents entity (`DomainWorkspace.DefaultModel` pair or fields, nullable agent provider/model) and the postgres store read/write paths for both
- [x] 1.3 Store/unit tests: workspace round-trips `default_model`; agent rows persist and read back with NULL pair; FK still rejects a pinned cross-workspace provider

## 2. Workspace default model API

- [x] 2.1 Workspace PATCH accepts `default_model {provider_id, model} | null` with both-or-neither validation (400 on half-set), same-workspace provider check (400 unknown), `workspace.write` permission, and the clear-blocked-while-inherited guard (422 with inherit-agent count)
- [x] 2.2 Add `default_model` to workspace read/switcher/boot payloads (`null` when unset); update handler payload structs and any workspace fixtures
- [x] 2.3 Handler tests: set, replace, clear (blocked and free), half-set 400, unknown provider 400, member 403; update smoke script with a default-model leg

## 3. Agent inheritance backend

- [x] 3.1 Agent create/patch validation: empty pair allowed only while a workspace default exists (422 otherwise), half-set pair 400; skip `RequiresMaxTokens` and effort-catalog validation for inherit agents
- [x] 3.2 Single effective-model resolver (agent pair, else workspace default) next to the runner; route `composeAgent`, promptgen/catalog preview handlers, heartbeat, scheduler, and channel template spawn through it (spawn keeps `gpt-4o` only when no workspace default)
- [x] 3.3 Runner behavior: fail-fast error naming the missing setting when an inherit agent resolves nothing; constant default max-tokens (4096) when the effective type requires one and the agent pins none
- [x] 3.4 Sweep for remaining raw `agent.ProviderID`/`agent.Model` consumers and convert stragglers; add runner tests (pinned unchanged, inherit resolves, inherit-without-default fails fast, max-tokens default)

## 4. Provider draft-verify API

- [x] 4.1 `POST /workspaces/:ws/providers/verify-draft`: draft-creds payload (`type`, `base_url?`, `key?`, `catalog_provider?`, `provider_id?`), stored-key fallback when key blank + config named, 400 when no credential resolves, `providers.write` guard, reuse per-type `Verify`, persist nothing
- [x] 4.2 Handler tests: typed-key verify, stored-key verify against submitted base URL, keyless 400, member 403, and an assertion that no row/state changed

## 5. Settings full-screen takeover

- [x] 5.1 `App.tsx`: stop rendering `Rail` on `/settings` routes; capture the last non-settings path (sessionStorage) as the layout's pathname changes
- [x] 5.2 `SettingsPage`: header row with `← Back` (navigates to the remembered path, fallback `/c`) + surface title above the section nav, at every viewport width; keep the section nav/column layout as-is
- [x] 5.3 Update `SettingsPage`/`App` tests (rail absence on settings, back navigation, deep-link fallback) and the layout snapshot fixtures that assumed the rail on settings

## 6. Workspace pane default model

- [x] 6.1 `WorkspaceSection`: replace the static `MODELS` select with provider select (configured providers) + `ModelCombobox`; wire into `saveWorkspace` PATCH (`default_model`), hydrate from the workspace payload, clear = both empty; surface 422 clear-blocked toast
- [x] 6.2 Component tests: save persists the pair (reload keeps), provider switch re-queries catalog and clears the model, clear-blocked toast, member read-only unaffected

## 7. Agent config inherit option

- [x] 7.1 `AgentConfigModal` Step 2: "Workspace default (inherit)" option in the provider select when the workspace default exists (hidden otherwise); selecting it hides model combobox + effort and saves the empty pair; drop the hard "Provider is required" error for that path
- [x] 7.2 Component tests: option visibility follows the workspace payload, inherit save sends the empty pair, pinned path unchanged

## 8. Provider dialog verify

- [x] 8.1 `ProviderFormDialog`: "Verify connection" button + inline busy/success/error strip calling `api.providers.verifyDraft`; disabled until a credential is expressible (typed key, or edit with stored key); save enablement unchanged
- [x] 8.2 Wire `verifyDraft` into `lib/api.ts`; component tests: verify success/error rendering, blank-key edit uses stored key, save never gated

## 9. Memory pane

- [x] 9.1 Flip the default tab to Configuration; add `SUPPORTED_EMBEDDING_DIMS` + `KNOWN_MODEL_DIMS` to `lib/embedding.ts`
- [x] 9.2 Replace the dimension number input with the Auto/768/1024/1536/2048/3072 dropdown: known-model preselect, Auto left unset until the test fills it, test-mismatch error rendering
- [x] 9.3 Update `MemoryPane` tests (opens on Configuration, dropdown options, preselect, test-discovery fill)

## 10. Verification

- [x] 10.1 `go build ./... && go vet ./... && go test ./...` green; integration tests against a live database where available
- [x] 10.2 Web build + component suites green; `openspec validate refactor-workspace-settings --strict` passes
- [x] 10.3 Live pass: set a default model, save an inherit agent, run it on the default; verify a draft provider from the dialog; confirm takeover/back behavior and the memory pane defaults
