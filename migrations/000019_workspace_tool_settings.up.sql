-- Workspace tool settings: one row per (workspace, tool) carrying the global
-- enable toggle and the tool's structured config. Absence of a row means the
-- tool is enabled with its default configuration (no backfill of defaults).
CREATE TABLE workspace_tool_settings (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tool_key     text NOT NULL,
    enabled      boolean NOT NULL DEFAULT true,
    config       jsonb NOT NULL DEFAULT '{}',
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, tool_key)
);

-- Seed the filesystem tool names into agents whose allowlist is empty so no
-- existing agent loses file capability when the fs tools become
-- allowlist-gated (design.md D3). Agents that already list tools are untouched.
UPDATE agents
SET tools = tools || ARRAY['ls', 'read_file', 'write_file', 'edit_file', 'glob', 'grep']
WHERE tools = '{}';
