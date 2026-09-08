-- Reverse 000024 (design.md D11): drop both MCP registry tables and restore
-- the dead disabled_mcps denylist. disabled_mcps is re-added empty — it was
-- read by nothing before this change (design.md D1), so there is nothing to
-- restore, and enabled_mcps selections have no pre-000024 representation.

ALTER TABLE agents
    ADD COLUMN disabled_mcps text[] NOT NULL DEFAULT '{}',
    DROP COLUMN enabled_mcps;

DROP TABLE IF EXISTS agent_mcp_servers;
DROP TABLE IF EXISTS workspace_mcp_servers;
