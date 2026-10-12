ALTER TABLE workspace_webhook_delivery_log
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS tokens_used;
