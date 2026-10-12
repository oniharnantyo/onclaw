-- Standalone workspace webhook endpoints and their delivery stores
-- (add-workspace-webhooks tasks.md 1.2, design.md D1/D4/D8).
--
-- One endpoint is its own aggregate row (D1 — not a recipe kind, not
-- connection columns): the auth credential's AES-256-GCM envelope with its
-- display-once last-4 hint (workspace AAD, the gateway credential
-- derivation), the declared event source (jsonb — header name, payload
-- field path, or fixed value), the declared delivery-id header the dedupe
-- keys on, the save-time-validated filter expression and turn template with
-- their sample payload, and the target binding (one agent plus a thread or
-- channel). The binding is NOT NULL: a standalone endpoint is born with it
-- (the store boundary enforces the complete shape; agent existence is the
-- stores'/service's check, mirroring the connection-webhook convention of
-- no FK on target_agent_id).
--
-- Purely additive: no writes to existing connection-webhook tables (design.md
-- migration plan).

CREATE TABLE workspace_webhooks (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    scheme text NOT NULL
        CHECK (scheme IN ('plain_token', 'hmac_sha256', 'timestamped_hmac')),
    secret_ciphertext text NOT NULL DEFAULT '',
    secret_hint text NOT NULL DEFAULT '',
    event_source jsonb NOT NULL DEFAULT '{}'::jsonb,
    delivery_id_source text NOT NULL DEFAULT '',
    filter_expression text NOT NULL DEFAULT '',
    turn_template text NOT NULL DEFAULT '',
    sample_payload text NOT NULL DEFAULT '',
    target_agent_id uuid NOT NULL,
    target_kind text NOT NULL
        CHECK (target_kind IN ('thread', 'channel')),
    target_id text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_workspace_webhooks_workspace_id
    ON workspace_webhooks(workspace_id);

-- Delivery dedupe (design.md D4): the same durable store the pipeline already
-- prunes hourly, keyed per endpoint. dedupe_key is the delivery id from the
-- endpoint's declared delivery-id source, or "sig:<hex>" — the hash of a
-- timestamped signature recorded to reject replays within the acceptance
-- skew. A row is written after signature verification (ack-after-persist),
-- so provider retries on timeout redeliver into an occupied key and are
-- acked without a second agent turn. created_at is the pruned window's
-- clock: rows older than the retention window are forgotten.
CREATE TABLE workspace_webhook_deliveries (
    webhook_id uuid NOT NULL REFERENCES workspace_webhooks(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    dedupe_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (webhook_id, dedupe_key)
);

-- The cleanup hook prunes by created_at; the index keeps the window sweep a
-- single range scan.
CREATE INDEX idx_workspace_webhook_deliveries_created_at
    ON workspace_webhook_deliveries(created_at);

-- Delivery log (design.md D8): one row per processed delivery — the
-- resolved event, the outcome (submitted with its run id, filtered,
-- duplicate, rejected, or failed), and a short detail reason. Retention is
-- bounded to the newest 100 rows per endpoint by the hourly sweep; the
-- (webhook_id, created_at DESC) index keeps the bounded list and the prune
-- cheap.
CREATE TABLE workspace_webhook_delivery_log (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_id uuid NOT NULL REFERENCES workspace_webhooks(id) ON DELETE CASCADE,
    event text NOT NULL DEFAULT '',
    outcome text NOT NULL,
    run_id text,
    detail text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_workspace_webhook_delivery_log_webhook_created
    ON workspace_webhook_delivery_log(webhook_id, created_at DESC);
