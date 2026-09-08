-- MCP server registries (design.md D2/D11): two scope tables sharing one
-- connection shape — the workspace-level registry and agent-private servers
-- (workspace_id kept NOT NULL because private rows belong to a
-- workspace-scoped agent; agents.id cascades the delete). env/headers are
-- JSONB arrays of {name, value} rows; values are encrypted envelopes treated
-- as opaque strings by the schema (the settings service owns the shape), so
-- plain jsonb with no db-level constraint. status/tool_count persist the last
-- probe or runtime connection attempt; no background health checks (design.md
-- D8), so status MAY be stale between probes.
--
-- agents.enabled_mcps (design.md D1) replaces the dead disabled_mcps denylist
-- with an opt-in allowlist of workspace MCP server ids; it starts empty so no
-- existing agent gains MCP tools until it subscribes.

CREATE TABLE workspace_mcp_servers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    transport    text NOT NULL,
    command      text NOT NULL DEFAULT '',
    args         text[] NOT NULL DEFAULT '{}',
    env          jsonb NOT NULL DEFAULT '[]',
    url          text NOT NULL DEFAULT '',
    headers      jsonb NOT NULL DEFAULT '[]',
    enabled      boolean NOT NULL DEFAULT true,
    status       text NOT NULL DEFAULT 'unknown',
    status_error text NOT NULL DEFAULT '',
    tool_count   int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_mcp_servers_workspace_id_name UNIQUE (workspace_id, name)
);

CREATE INDEX idx_workspace_mcp_servers_workspace_id ON workspace_mcp_servers(workspace_id);

CREATE TABLE agent_mcp_servers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name         text NOT NULL,
    transport    text NOT NULL,
    command      text NOT NULL DEFAULT '',
    args         text[] NOT NULL DEFAULT '{}',
    env          jsonb NOT NULL DEFAULT '[]',
    url          text NOT NULL DEFAULT '',
    headers      jsonb NOT NULL DEFAULT '[]',
    enabled      boolean NOT NULL DEFAULT true,
    status       text NOT NULL DEFAULT 'unknown',
    status_error text NOT NULL DEFAULT '',
    tool_count   int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_agent_mcp_servers_agent_id_name UNIQUE (agent_id, name)
);

CREATE INDEX idx_agent_mcp_servers_agent_id ON agent_mcp_servers(agent_id);
CREATE INDEX idx_agent_mcp_servers_workspace_id ON agent_mcp_servers(workspace_id);

ALTER TABLE agents
    ADD COLUMN enabled_mcps text[] NOT NULL DEFAULT '{}',
    DROP COLUMN disabled_mcps;
