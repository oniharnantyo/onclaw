-- MCP connection auth mode (add-mcp-oauth-client proposal/design.md D4,
-- tasks.md 2.2b): both MCP server scope tables gain the auth-mode column and
-- the bring-your-own OAuth client rows. none is the pre-OAuth default — every
-- existing row backfills to it, so no server changes behavior (proposal:
-- "No behavioral change for existing servers (auth mode defaults to none)").
--
-- auth_mode carries a catalog CHECK (the 000063 connection-status precedent
-- for enum-like columns); the store normalizes the domain's empty "none"
-- marker to the explicit default on write, so the column only ever holds
-- catalog values. The BYO client id and secret ride the established sibling
-- convention for optional string columns (command/url/status_error) and for
-- secret material (env/headers): text NOT NULL DEFAULT '' — the secret is an
-- opaque string at this layer, an AES-256-GCM envelope produced by the
-- service layer exactly like the env/header values, never a plaintext. The
-- BYO columns are inert unless auth_mode = oauth.

ALTER TABLE workspace_mcp_servers
    ADD COLUMN auth_mode text NOT NULL DEFAULT 'none'
        CHECK (auth_mode IN ('none', 'oauth')),
    ADD COLUMN oauth_client_id text NOT NULL DEFAULT '',
    ADD COLUMN oauth_client_secret text NOT NULL DEFAULT '';

ALTER TABLE agent_mcp_servers
    ADD COLUMN auth_mode text NOT NULL DEFAULT 'none'
        CHECK (auth_mode IN ('none', 'oauth')),
    ADD COLUMN oauth_client_id text NOT NULL DEFAULT '',
    ADD COLUMN oauth_client_secret text NOT NULL DEFAULT '';
