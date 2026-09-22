## Why

The connections gallery (add-workspace-connections) renders OAuth-only services — Atlassian (Jira + Confluence), Slack, Linear — as coming-soon cards because a pasted token structurally cannot serve OAuth 2.0 (3LO): these services require browser consent, issue expiring tokens, and expect refresh. Until OAuth exists, the entire Atlassian class has no path in (their remote MCP server rejects API tokens). This change activates the coming-soon cards.

## What Changes

- **Recipes may declare auth kind `oauth`**: authorize endpoint, token endpoint, scopes per access level, and app-registration guidance, alongside the existing PAT fields. A recipe flips its card from coming-soon to available when its auth kind is `oauth` and the instance has a registered app for the provider.
- **Instance-level OAuth app registration**: an instance admin registers one app per provider (client id + client secret, encrypted instance-scoped) and gets the exact redirect URI to configure at the provider. Workspaces authorize through the shared app; tokens remain per-connection.
- **Connect via redirect**: the Integrate button issues an authorize redirect instead of a token form; after provider consent, the callback exchanges the code, runs the recipe's probe, and activates the connection — same probe-gated, store-nothing-on-failure hygiene as PAT connect.
- **Token lifecycle**: refresh token + expiry + granted scopes stored encrypted on the connection; refresh is triggered when a credential resolution happens within the refresh margin, and a successful refresh writes the new access token through the existing secret-row machinery (the materialized server's `Authorization` row) so the MCP runtime is untouched. A failed refresh flips the connection to an `expired` status.
- **Reauthorization**: an expired connection offers a Reauthorize action that re-runs the consent flow, replacing the token set in place (connection id, materialized server, attachments preserved).
- **Atlassian ships as the reference OAuth recipe** (one connection covering Jira and Confluence); Slack and Linear recipes are declared and verify at apply time.

## Capabilities

### New Capabilities

<!-- None: OAuth extends the existing connection capability and instance settings. -->

### Modified Capabilities

- `workspace-connections`: ADDED requirements — OAuth connect flow (redirect, consent, callback, probe-gated activation), token refresh lifecycle, reauthorization of expired connections, and recipe availability gated on a registered instance app.
- `instance-admin`: ADDED requirement — per-provider OAuth app registration (encrypted client credentials, redirect URI display).

## Impact

- **Domain** (`internal/domain`): auth-kind `oauth` on recipes; connection token-lifecycle fields (refresh envelope, expiry, granted scopes, status `expired`).
- **Store** (`internal/store`, `internal/store/postgres`): migration — token-lifecycle columns on `workspace_connections`, new instance OAuth apps table; fakes updated.
- **Services/handlers**: connections service OAuth flow (authorize URL builder, callback exchange, refresh-on-resolution); instance settings handler for app registration; callback route.
- **Instance secrets**: client secrets encrypted with the instance-scoped key derivation.
- **MCP runtime** (`internal/agents/mcp`): no changes — write-through refresh keeps the materialized server's secret row authoritative.
- **Web**: OAuth connect variant of the dialog (redirect hand-off), expired status + Reauthorize action on connected cards, instance admin OAuth apps pane.
- **Smoke tests**: OAuth flow coverage with a stub provider.
