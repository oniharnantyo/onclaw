DROP TABLE IF EXISTS session_checkpoints;
DROP TABLE IF EXISTS session_events;

CREATE TABLE workspace_skills (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    body         text NOT NULL DEFAULT '',
    enabled      boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_skills_workspace_id_name UNIQUE (workspace_id, name)
);

CREATE INDEX idx_workspace_skills_workspace_id ON workspace_skills(workspace_id);

ALTER TABLE workspaces
    DROP COLUMN IF EXISTS description;

ALTER TABLE agents
    ADD COLUMN tools text[] NOT NULL DEFAULT '{}',
    ADD COLUMN skills text[] NOT NULL DEFAULT '{}',
    ADD COLUMN mcp text[] NOT NULL DEFAULT '{}';

ALTER TABLE agents
    DROP COLUMN IF EXISTS context_window,
    DROP COLUMN IF EXISTS disabled_tools,
    DROP COLUMN IF EXISTS disabled_skills,
    DROP COLUMN IF EXISTS disabled_mcps;
