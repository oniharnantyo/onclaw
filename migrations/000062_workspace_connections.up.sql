-- Workspace service connections (add-workspace-connections design.md D1/D8,
-- tasks.md 1.3): the product objects behind the Integrations gallery. A
-- connection owns a materialized ordinary workspace MCP server — the server
-- row carries the nullable origin_connection_id marker below — so dialing,
-- caching, probing, provider-safe naming, policy, and per-agent attachment all
-- ride the existing MCP runtime unchanged. The token itself is the server's
-- encrypted secret header row (the settings service owns the shape); the
-- connection row is credential-free by construction.
--
-- One connection per service per workspace (design.md D6): unique
-- (workspace_id, service). service is the recipe id from the server-side
-- recipe registry (e.g. 'github'); access_level is connection metadata
-- (design.md D5) driving guided token scopes and gallery display — actual
-- write enforcement rides the token's own scopes.

CREATE TABLE workspace_connections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    service      text NOT NULL,
    access_level text NOT NULL CHECK (access_level IN ('read_only', 'read_write')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_connections_workspace_id_service UNIQUE (workspace_id, service)
);

CREATE INDEX idx_workspace_connections_workspace_id ON workspace_connections(workspace_id);

-- Origin marker (design.md D1): nullable — hand-made servers stay unmarked and
-- fully editable (design.md D11); marked rows are read-plus-probe only outside
-- Integrations, and disconnect cascades them. ON DELETE CASCADE keeps the DB
-- consistent even when the connection row is removed outside the store's
-- cascade path.
ALTER TABLE workspace_mcp_servers
    ADD COLUMN origin_connection_id uuid REFERENCES workspace_connections(id) ON DELETE CASCADE;

CREATE INDEX idx_workspace_mcp_servers_origin_connection_id
    ON workspace_mcp_servers(origin_connection_id);

-- Backfill integrations.write into the built-in roles that grant it since the
-- service-connections feature. Built-in role permissions are copied from
-- catalog constants into role rows at workspace creation with no runtime
-- derivation, so roles created before this migration keep the older permission
-- snapshot (design.md D10 — the gateways.write precedent, 000049). Superadmin,
-- Owner, and Admin gain it; Member never; custom roles only on explicit grant.

UPDATE roles
SET permissions = array_append(permissions, 'integrations.write')
WHERE built_in = true AND name IN ('Superadmin', 'Owner', 'Admin')
  AND NOT ('integrations.write' = ANY(permissions));
