# Design

## Context

All MCP server read paths (`MCPServer`, `MCPServers`, `UpdateMCPServer`, `ProbeMCPServer`, `ConnectIntegration`, `DisconnectMCPServer`) already funnel through one redaction helper, `redactIntegrationCredential` (`internal/services/settings_mcp.go:203`), which clears `Auth.IntegrationCredential` and computes `CredentialSet`/`CredentialHint` — plain row fields on `store.MCPServer` (`internal/store/types.go:114-120`), never persisted, populated on read. `Auth.ClientSecret` survives that helper and reaches the frontend.

The web_search chain is edited through `WebSearchConfig`/`WebSearchChainEntry` (`internal/services/settings_websearch.go`), which is the persisted runtime document surfaced verbatim — including `APIKey`. The save path is create-or-replace and currently *requires* a key on every save, which is why the key round-trips.

Constraints that shape the fix:
- `MCPServerAuth` is persisted by marshaling the whole struct (`internal/store/sqlite/mcpservers.go:121`), so a `json:"-"` on `ClientSecret` would silently drop DCR-issued secrets from storage on the next row rewrite. Redaction must stay at the service boundary.
- The MCP pane's edit dialog has no client-id/secret inputs (`McpPane.tsx` auth section is a mode select only) — client secrets are set exclusively through the integrations connect sheet (`IntegrationConnectInput`, a separate input struct) or DCR (`settings_mcp_oauth.go:152`). The integrations sheet already starts every edit with an empty secret field and relies on `withStoredAuth` blank-preservation (`settings_mcp.go:430`).
- The runtime consumes stored rows/tool_settings directly (`internal/agents/mcp.go:65`, the tools package's lazy resolver) and never these service views.

## Goals / Non-Goals

**Goals:**
- No settings-service read or listing returns a client secret or a chain API key; only set-state plus a last-4 hint crosses the bridge.
- Edits made after redaction preserve stored secrets (no wipe-on-edit regression).
- Storage format, runtime behavior, and the authorize/DCR flows are untouched.

**Non-Goals:**
- No encryption-at-rest change (store-level, per `store/types.go:86` single-user assumption).
- No client-id/secret inputs added to the MCP pane dialog.
- No masking for `WebSearchChainEntry.BaseURL` (an instance URL, not a credential).
- The other audit findings (unbounded lists, `SettingsService` split, `SaveGateway` validation) are separate changes.

## Decisions

**D1 — Redact at the existing choke point, not per-method.** `redactIntegrationCredential` additionally clears `Auth.ClientSecret` and computes `ClientSecretSet`/`ClientSecretHint`, added as plain row fields next to `CredentialSet`/`CredentialHint` (`store/types.go`). The helper is renamed to `redactServerSecrets` since it now covers every secret class on the row. Rationale: all six read paths already call it, so one edit covers hand-created and integration-created definitions alike; per-method redaction invites the next new read method to forget. Alternative rejected: `json:"-"` on the auth field — breaks persistence (see Context).

**D2 — Blank-preserves `ClientSecret` in `UpdateMCPServer`.** Same shape as the existing `IntegrationCredential` preserve (`settings_mcp.go:120`): when the incoming `Auth.ClientSecret` is empty, copy the stored row's before persisting. This is load-bearing — the MCP pane round-trips `Auth` wholesale from the redacted read (`McpPane.tsx` dialog draft), so without the preserve, any edit would wipe the secret. Non-blank replaces, mirroring the spec's token-credential rule. `CreateMCPServer` is unchanged (fresh rows; secrets arrive via ConnectIntegration or DCR).

**D3 — Web-search read model carries `KeySet`/`KeyHint`, not `APIKey`.** `WebSearchChainEntry` keeps `ID`/`Name`/`Provider`/`BaseURL` and gains `KeySet bool` + `KeyHint string`; the `APIKey` field is removed from the read model. The hint reuses the existing `keyHint` helper (last 4 runes). Alternative rejected: keeping the round-trip (status quo) — violates the house secrecy pattern every other credential family follows (`ProviderView`, `ChannelView`, `VaultLogin`, `CredentialHint`).

**D4 — Save merges by stable ID, validates after the merge.** `SaveWebSearchConfig` loads the stored config first. For each incoming entry: a blank `APIKey` with a stable ID resolves to the stored key; a blank key with no ID (new entry) is rejected unchanged; a non-blank key replaces. The provider-catalog and credential-required validations then run on the *merged* entry, so an entry that changes provider while leaving the key blank keeps the stored credential and is validated against the new provider's requirement. Entry IDs already survive reorders (`nextChainEntryID` mints only for empty IDs), so the merge key is stable. Alternative rejected: per-entry partial-update methods — over-models a four-entry chain and breaks the create-or-replace contract the pane is built on.

**D5 — Frontend treats both fields as write-only with hint display.** Tools pane chain editor: the key input renders empty with a placeholder showing `KeySet`/`KeyHint` ("Saved ··abcd — leave blank to keep"), matching the provider detail view's mono-face key-hint convention (settings spec, "Provider row identity"). IntegrationsPane: the edit sheet gains a "configured (···abcd)" hint line under the client-secret field when `ClientSecretSet`; the field itself stays write-only as today. Bindings regenerate; `client.test.ts` mocks widen (known regen gotcha).

## Risks / Trade-offs

- [Any new MCP read method added later forgets redaction] → The choke point is a single named helper with a doc comment stating the invariant; a service-level test asserts no read path leaks either secret field (grep the generated models for `client_secret`/`api_key` as a guard, mirroring the `scan_chunks.py`-style gate idea).
- [Stored key vs changed provider mismatch (D4)] → The merged entry is validated against the new provider's requirement; a key kept across a provider change is the same semantic `UpdateProvider` already has (blank key keeps stored across a type change). Documented in the spec scenario.
- [Frontend code still referencing `entry.APIKey` after regen] → TS build fails loudly at the dropped field; the change's task list includes fixing the chain editor in the same pass.
- [Undoing a redaction by typing the old value back] → Not a regression: users re-entering a key replace it, identical to providers today.

## Migration Plan

No stored-format change; nothing to migrate. Rollout is: implement Go changes, regenerate bindings (`task`-driven build regenerates into `frontend/bindings/`), update the two frontend surfaces, restart the dev app. Rollback is reverting the commit — stored secrets and keys were never touched.

## Open Questions

(none)
