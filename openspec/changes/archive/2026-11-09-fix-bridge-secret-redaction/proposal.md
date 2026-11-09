# Proposal

## Why

Two secret classes cross the Go↔JS bridge unredacted, against the house rule that only a set-flag plus a last-four hint ever reaches the frontend (`integrations` spec requirement 5 already mandates this for client secrets — the code violates it): MCP OAuth `ClientSecret` rides every `MCPServer`/`MCPServers`/`ProbeMCPServer`/`ConnectIntegration`/`DisconnectMCPServer` read inside `MCPServerAuth` (`store/types.go:164`), and the web_search chain read returns each entry's `APIKey` verbatim (`settings_websearch.go:86`). Found in the Wails bindings best-practices audit (2026-11-06).

## What Changes

- `redactIntegrationCredential` (the single choke point all MCP server read paths already funnel through) additionally clears `Auth.ClientSecret` and computes `ClientSecretSet` / `ClientSecretHint` view fields on `store.MCPServer`, mirroring the existing `CredentialSet`/`CredentialHint` pair. Applies to every read path — integration-created definitions and hand-created OAuth definitions alike.
- `UpdateMCPServer` blank-preserves `Auth.ClientSecret` exactly as it already blank-preserves `Auth.IntegrationCredential` — required once reads are redacted, or any MCP-pane edit round-trip would wipe the stored secret (the pane sends `Auth` back wholesale).
- The web_search chain read model stops carrying `APIKey`: `WebSearchChainEntry` gains `KeySet`/`KeyHint`, `BaseURL` (not a secret) continues to round-trip.
- `SaveWebSearchConfig` blank-preserves the stored API key for an entry that carries a stable ID with a blank key; new entries (no ID) and provider changes still require a key — validation runs after the merge.
- Frontend: the Tools pane chain editor renders the hint and treats the key field as write-only (blank = keep); the Integrations connect sheet (already write-only) gains a "configured (···xy)" hint line for the client secret on edit.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `integrations`: requirement 5 (credential redaction) is MODIFIED — it names the pre-registered client secret explicitly as an `MCPServerAuth` field that MUST be redacted on every settings-service read or listing of an MCP server definition, whatever created the definition, and specifies the `ClientSecretSet`/hint exposure and the update blank-preserves rule.
- `settings`: the Tools pane requirement (web_search chain) is MODIFIED — chain reads expose `KeySet` + last-four `KeyHint` instead of the API key; saving an entry whose key field is blank with a stable ID keeps the stored credential.

## Impact

- Go: `internal/store/types.go` (`MCPServer` view fields), `internal/services/settings_mcp.go` (redaction choke point, `UpdateMCPServer`), `internal/services/settings_websearch.go` (read/save models, merge), generated bindings regenerate (`frontend/bindings/.../store/models.ts` drops `client_secret`, `services/models.ts` chain entry shape).
- Frontend: `features/settings/components/ToolsPane` (chain editor), `features/integrations/IntegrationsPane.tsx` (hint line), `lib/client.ts` type re-exports.
- Runtime behavior unchanged: the agent runtime reads tool_settings and `store.MCPServer` rows directly (`internal/agents/mcp.go`), not through these service views.
- No migration: stored values are untouched; only the read shape changes.
