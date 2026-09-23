-- Reverse of 000069_mcp_server_auth_mode.up.sql. OAuth-mode servers' BYO
-- client rows are lost on rollback by design — client secrets are never
-- exported (the same convention as the connection refresh envelopes); the
-- server rows survive, reverting to the static-rows-only behavior every
-- pre-OAuth server had.

ALTER TABLE agent_mcp_servers
    DROP COLUMN IF EXISTS oauth_client_secret,
    DROP COLUMN IF EXISTS oauth_client_id,
    DROP COLUMN IF EXISTS auth_mode;

ALTER TABLE workspace_mcp_servers
    DROP COLUMN IF EXISTS oauth_client_secret,
    DROP COLUMN IF EXISTS oauth_client_id,
    DROP COLUMN IF EXISTS auth_mode;
