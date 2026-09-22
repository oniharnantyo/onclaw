-- Reverse of 000062_workspace_connections.up.sql. A created connection's data
-- is lost on rollback by design (design.md D8: tokens are deleted, never
-- archived — nothing is exported).

-- Remove the integrations.write permission from built-in roles.
UPDATE roles
SET permissions = array_remove(permissions, 'integrations.write')
WHERE built_in = true;

DROP INDEX IF EXISTS idx_workspace_mcp_servers_origin_connection_id;
ALTER TABLE workspace_mcp_servers
    DROP COLUMN IF EXISTS origin_connection_id;

DROP TABLE IF EXISTS workspace_connections;
