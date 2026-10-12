-- Reverse of 000075_workspace_webhooks.up.sql. Endpoints, their dedupe
-- window, and their delivery history are lost on rollback by design
-- (design.md migration plan): secrets are deleted, never archived —
-- disabled-by-rollback endpoints simply stop ingesting, and
-- operator-configured provider webhooks fail delivery until they are
-- reconfigured or removed.

DROP INDEX IF EXISTS idx_workspace_webhook_delivery_log_webhook_created;
DROP TABLE IF EXISTS workspace_webhook_delivery_log;

DROP INDEX IF EXISTS idx_workspace_webhook_deliveries_created_at;
DROP TABLE IF EXISTS workspace_webhook_deliveries;

DROP INDEX IF EXISTS idx_workspace_webhooks_workspace_id;
DROP TABLE IF EXISTS workspace_webhooks;
