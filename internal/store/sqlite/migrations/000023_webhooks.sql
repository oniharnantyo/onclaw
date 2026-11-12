-- Schema v23: webhooks (add-webhook-triggers design D7). Three tables —
-- endpoints, their ordered verifier children, and the bounded per-endpoint
-- delivery log — plus the listener address joining the gateway's singleton
-- trigger-config row. Conventions as in every schema: store-assigned hex
-- ids, 0/1 INTEGER booleans, RFC3339Nano TEXT timestamps, secrets in the
-- clear (channels.bot_token precedent: single-user app, write-only through
-- the service layer). name carries COLLATE NOCASE so the unique index
-- rejects duplicates case-insensitively (schedules precedent). agent_id and
-- group_id are plain TEXT, NOT foreign keys: an endpoint must survive its
-- target's deletion so dispatch can record the dead target
-- (add-webhook-triggers task 5.4) — the agent_groups precedent for columns
-- that outlive the referenced row. Verifier engine config is explicit
-- per-engine columns, not JSON: per-verifier secret handling (write-only,
-- blank-keeps) needs column-level care (design D7). The delivery log is
-- bounded per endpoint by the store (oldest evicted on append, ~50), so
-- there is no time-based prune. No backfill; rollback is DROP TABLE in
-- reverse creation order plus ALTER TABLE DROP COLUMN webhook_addr.

CREATE TABLE webhook_endpoints (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL COLLATE NOCASE,
    agent_id   TEXT,
    group_id   TEXT,
    prompt     TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((agent_id IS NULL) <> (group_id IS NULL))
);

CREATE UNIQUE INDEX idx_webhook_endpoints_name ON webhook_endpoints(name);

CREATE TABLE webhook_verifiers (
    id               TEXT PRIMARY KEY,
    endpoint_id      TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    position         INTEGER NOT NULL DEFAULT 0,
    kind             TEXT NOT NULL CHECK (kind IN ('path', 'header', 'raw-hmac', 'stdweb', 'ts-hmac')),
    preset           TEXT NOT NULL DEFAULT '',
    header_name      TEXT NOT NULL DEFAULT '',
    algo             TEXT NOT NULL DEFAULT '',
    encoding         TEXT NOT NULL DEFAULT '',
    prefix           TEXT NOT NULL DEFAULT '',
    scheme           TEXT NOT NULL DEFAULT '',
    timestamp_header TEXT NOT NULL DEFAULT '',
    secret           TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_webhook_verifiers_endpoint
    ON webhook_verifiers (endpoint_id, position);

CREATE TABLE webhook_deliveries (
    id                 TEXT PRIMARY KEY,
    endpoint_id        TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    attempted_at       TEXT NOT NULL,
    status             INTEGER NOT NULL,
    outcome            TEXT NOT NULL,
    session_id         TEXT NOT NULL DEFAULT '',
    message_id         TEXT NOT NULL DEFAULT '',
    via                TEXT NOT NULL DEFAULT '',
    sender_delivery_id TEXT NOT NULL DEFAULT '',
    note               TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL
);

CREATE INDEX idx_webhook_deliveries_endpoint
    ON webhook_deliveries (endpoint_id, attempted_at DESC);

-- The webhook listener address rides the gateway's singleton trigger
-- configuration (data-store spec: stored with the gateway's trigger
-- configuration); '' means unset — callers apply the loopback default.
ALTER TABLE gateway_settings ADD COLUMN webhook_addr TEXT NOT NULL DEFAULT '';
