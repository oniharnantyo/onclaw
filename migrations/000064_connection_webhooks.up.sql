-- Connection webhook state and delivery dedupe
-- (add-connection-webhooks design.md D2/D3, tasks.md 1.3).
--
-- workspace_connections gains the webhook configuration columns: the enable
-- toggle, the HMAC secret's AES-256-GCM envelope with its display-once
-- last-4 hint (workspace AAD, the gateway credential derivation), the target
-- binding (one agent plus a thread or channel), and the selected subset of
-- the recipe's event catalog. All seven are inert for existing connections:
-- disabled, empty envelopes and hints, unset target, empty selection — no
-- backfill, no dual-write (design.md migration plan step 1). A connection
-- whose recipe declares no webhook support simply never writes them.
--
-- webhook_target_agent_id is the one nullable column: the binding is unset
-- until the first enablement configures it. The empty target kind is the
-- unset shape; enabled rows always carry a complete binding (enforced by the
-- store boundary, backstopped here by the catalog CHECK).

ALTER TABLE workspace_connections
    ADD COLUMN webhook_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN webhook_secret_ciphertext text NOT NULL DEFAULT '',
    ADD COLUMN webhook_secret_hint text NOT NULL DEFAULT '',
    ADD COLUMN webhook_target_agent_id uuid,
    ADD COLUMN webhook_target_kind text NOT NULL DEFAULT ''
        CHECK (webhook_target_kind IN ('', 'thread', 'channel')),
    ADD COLUMN webhook_target_id text NOT NULL DEFAULT '',
    ADD COLUMN webhook_events text[] NOT NULL DEFAULT '{}';

-- Delivery dedupe (design.md D3): recent delivery ids in a dedicated table,
-- delivery id unique per connection — the primary key. A row is written
-- after signature verification (ack-after-persist: before run completion), so
-- provider retries on timeout redeliver into an occupied id and are acked
-- without a second agent turn. created_at is the pruned window's clock: rows
-- older than the retention window are forgotten and a that-late redelivery
-- is a new event, not a replay.
CREATE TABLE connection_webhook_deliveries (
    connection_id uuid NOT NULL REFERENCES workspace_connections(id) ON DELETE CASCADE,
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    delivery_id   text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, delivery_id)
);

-- The cleanup hook prunes by created_at; the index keeps the window sweep a
-- single range scan.
CREATE INDEX idx_connection_webhook_deliveries_created_at
    ON connection_webhook_deliveries(created_at);
