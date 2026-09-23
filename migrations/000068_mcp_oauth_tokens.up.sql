-- MCP OAuth token rows (add-mcp-oauth-client design.md D4, tasks.md 2.2):
-- one stored OAuth token set per oauth-mode MCP server row, keyed by scope
-- kind (workspace | agent) + server id, separate from the user-visible
-- header/env rows so the UI's secret-row model (hint/last-4, manual edit)
-- stays meaningful while refresh mutates the tokens invisibly.
--
-- The scope is a two-column server pointer with an exactly-one CHECK:
-- ws_server_id for workspace-registered servers (agent_id NULL),
-- agent_server_id for agent-private servers (agent_id set). Both pointers
-- and the agent carry ON DELETE CASCADE — a server row's deletion takes its
-- token row with it, and a deleted agent's private servers (and their token
-- rows) are already gone. workspace_id is the tenant partition: no token
-- query runs without it.
--
-- Token material exists only as AES-256-GCM envelopes under the instance
-- master key with the workspace id as AAD — the same derivation as the
-- connection refresh ciphertexts (000063). There is no hint column: presence
-- is the only client-visible fact about these rows.

CREATE TABLE mcp_oauth_tokens (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id           uuid REFERENCES agents(id) ON DELETE CASCADE,
    ws_server_id       uuid REFERENCES workspace_mcp_servers(id) ON DELETE CASCADE,
    agent_server_id    uuid REFERENCES agent_mcp_servers(id) ON DELETE CASCADE,
    access_ciphertext  text NOT NULL,
    refresh_ciphertext text NOT NULL DEFAULT '',
    expires_at         timestamptz,
    granted_scopes     text[] NOT NULL DEFAULT '{}',
    issuer             text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    -- One token set per server row. NULLs are distinct in Postgres unique
    -- indexes, so each constraint binds only its own scope kind.
    CONSTRAINT uq_mcp_oauth_tokens_ws_server UNIQUE (ws_server_id),
    CONSTRAINT uq_mcp_oauth_tokens_agent_server UNIQUE (agent_server_id),
    CONSTRAINT ck_mcp_oauth_tokens_scope CHECK (
        (ws_server_id IS NOT NULL AND agent_server_id IS NULL AND agent_id IS NULL)
        OR (ws_server_id IS NULL AND agent_server_id IS NOT NULL AND agent_id IS NOT NULL)
    )
);

CREATE INDEX idx_mcp_oauth_tokens_workspace_id ON mcp_oauth_tokens(workspace_id);
