-- Reverse of 000065_connection_webhook_last_error.up.sql. The render-failure
-- residue is diagnostic state only — dropping it loses nothing but the last
-- error display.

ALTER TABLE workspace_connections
    DROP COLUMN IF EXISTS webhook_last_error;
