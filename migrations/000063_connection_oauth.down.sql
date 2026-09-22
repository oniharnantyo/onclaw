-- Reverse of 000063_connection_oauth.up.sql. In-flight OAuth connections'
-- refresh state (refresh envelope, expiry, granted scopes) is lost on
-- rollback by design — tokens are never exported (design.md migration plan
-- step 3); the connections and their materialized servers survive as rows,
-- reverting to the connected status their PAT-era predecessors carried.

DROP TABLE IF EXISTS instance_oauth_apps;

ALTER TABLE workspace_connections
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS granted_scopes,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS refresh_ciphertext;
