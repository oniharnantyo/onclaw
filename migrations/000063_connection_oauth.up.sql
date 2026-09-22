-- Connection OAuth token lifecycle and instance OAuth apps
-- (add-connection-oauth design.md D1/D2/D6, tasks.md 1.3).
--
-- workspace_connections gains the OAuth token-lifecycle columns: the
-- refresh-token ciphertext envelope (the access token itself stays in the
-- materialized server's encrypted Authorization row — change-1 D4 — so the
-- write-through refresh needs no MCP-runtime change), the access-token
-- expiry, the consented scopes, and the persisted status. All four are inert
-- for existing PAT connections: the envelope defaults empty, expiry and
-- scopes stay empty, and status defaults connected — no backfill, no
-- dual-write (design.md migration plan step 2).
--
-- status is the first-class persisted connection status (design.md D6):
-- connected and error mirror the materialized server's probe statuses;
-- expired is the OAuth-only recovery state a failed refresh enters and only
-- reauthorization clears. Existing rows are live connections, hence the
-- connected default.

ALTER TABLE workspace_connections
    ADD COLUMN refresh_ciphertext text NOT NULL DEFAULT '',
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN granted_scopes text[] NOT NULL DEFAULT '{}',
    ADD COLUMN status text NOT NULL DEFAULT 'connected'
        CHECK (status IN ('connected', 'error', 'expired'));

-- Instance OAuth apps (design.md D2): one registered provider app per
-- instance, shared by every workspace's authorization flows. provider is the
-- identity (provider-unique — the connections gallery's availability signal
-- and the callback flow's app lookup key). The client secret is stored only
-- as the instance-scoped AES-256-GCM envelope (instance master key, empty
-- AAD — the instance-hooks derivation) plus its last-4 hint; the redirect
-- URI is derived from the instance public base URL at read time and is
-- deliberately not stored.
CREATE TABLE instance_oauth_apps (
    provider                 text PRIMARY KEY,
    client_id                text NOT NULL,
    client_secret_ciphertext text NOT NULL,
    client_secret_hint       text NOT NULL DEFAULT '',
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
