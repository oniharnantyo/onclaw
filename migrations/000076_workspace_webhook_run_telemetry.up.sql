-- add-webhook-channel-visibility 4.1 (design.md D5): the submitted
-- delivery-log row doubles as the run record — three telemetry columns the
-- submitter's drain finishes. Defaults backfill legacy rows as blank/zero;
-- readers treat blank status as completed (the fanout's
-- success-by-default).
ALTER TABLE workspace_webhook_delivery_log
    ADD COLUMN status text NOT NULL DEFAULT '',
    ADD COLUMN duration_ms bigint NOT NULL DEFAULT 0,
    ADD COLUMN tokens_used integer NOT NULL DEFAULT 0;
