-- Reverse of 000068_mcp_oauth_tokens.up.sql. Stored OAuth token sets are lost
-- on rollback by design — tokens are deleted, never exported (add-mcp-oauth-client
-- migration plan); the server rows survive and simply need (re)authorization
-- on the next dial.

DROP TABLE IF EXISTS mcp_oauth_tokens;
