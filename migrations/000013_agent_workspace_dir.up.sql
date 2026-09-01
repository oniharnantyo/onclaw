-- Per-agent on-disk workspace directory (relative path), e.g.
-- .onclaw/workspaces/<tenant_slug>/agents/<agent_slug>
ALTER TABLE agents ADD COLUMN workspace_dir text NOT NULL DEFAULT '';
