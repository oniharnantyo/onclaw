-- Reverse of 000064_connection_webhooks.up.sql. Webhook configuration and the
-- delivery-dedupe window are lost on rollback by design (design.md migration
-- plan step 2): secrets are deleted, never archived — disabled-by-rollback
-- webhooks simply stop ingesting, and operator-configured provider webhooks
-- fail delivery until they are reconfigured or removed.

DROP INDEX IF EXISTS idx_connection_webhook_deliveries_created_at;
DROP TABLE IF EXISTS connection_webhook_deliveries;

ALTER TABLE workspace_connections
    DROP COLUMN IF EXISTS webhook_events,
    DROP COLUMN IF EXISTS webhook_target_id,
    DROP COLUMN IF EXISTS webhook_target_kind,
    DROP COLUMN IF EXISTS webhook_target_agent_id,
    DROP COLUMN IF EXISTS webhook_secret_hint,
    DROP COLUMN IF EXISTS webhook_secret_ciphertext,
    DROP COLUMN IF EXISTS webhook_enabled;
