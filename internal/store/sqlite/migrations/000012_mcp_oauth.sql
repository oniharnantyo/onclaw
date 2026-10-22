-- Schema v12: MCP OAuth 2.1 support (mcp-oauth design D4). auth holds the
-- server's authentication settings as a JSON object ('{}' = mode none, so
-- existing rows read as unauthenticated). mcp_oauth_tokens stores one token
-- set per server, keyed by server id; the foreign-key cascade removes a
-- server's tokens with the server itself. No backfill.
ALTER TABLE mcp_servers ADD COLUMN auth TEXT NOT NULL DEFAULT '{}';
CREATE TABLE mcp_oauth_tokens (
    server_id  TEXT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
    token      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
