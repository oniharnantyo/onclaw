-- Workspace API keys authenticate /v1 (OpenResponses) requests and carry the
-- workspace (tenant) scope; the creating user is recorded for attribution.
-- Plaintext keys are never stored — only the SHA-256 hash plus display fields.
CREATE TABLE workspace_api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    key_hash     text NOT NULL,
    key_prefix   text NOT NULL,
    key_suffix   text NOT NULL,
    created_by   uuid NOT NULL REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz
);

-- Lookup by hash happens on every /v1 request; the hash is globally unique.
CREATE UNIQUE INDEX idx_workspace_api_keys_key_hash ON workspace_api_keys(key_hash);
CREATE INDEX idx_workspace_api_keys_workspace_id ON workspace_api_keys(workspace_id);
