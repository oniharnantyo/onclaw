CREATE TABLE workspace_providers (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type           text NOT NULL,
    name           text NOT NULL,
    base_url       text NOT NULL DEFAULT '',
    key_ciphertext text NOT NULL DEFAULT '',
    key_hint       text NOT NULL DEFAULT '',
    enabled        boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_workspace_providers_workspace_id ON workspace_providers(workspace_id);
