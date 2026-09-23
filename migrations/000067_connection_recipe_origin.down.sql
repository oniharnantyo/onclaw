-- Reverse of 000067_connection_recipe_origin.up.sql: drop the (workspace,
-- service, origin) unique index and the origin column, restoring 000062's
-- one-connection-per-service constraint. Downgrading a schema that already
-- holds two origins for one service fails the constraint restore — collapse
-- such rows to one per (workspace, service) first; pre-000067 data always
-- satisfies that.

DROP INDEX IF EXISTS uq_workspace_connections_workspace_service_origin;

ALTER TABLE workspace_connections
    DROP COLUMN IF EXISTS origin;

ALTER TABLE workspace_connections
    ADD CONSTRAINT uq_workspace_connections_workspace_id_service UNIQUE (workspace_id, service);
