-- Reverse 000025 (design.md D1/D5): recreate agent_user_memories empty — it
-- was provably never written (no runtime or API writer existed), so there is
-- no data to restore — drop the two new memory tables and the agents unique
-- constraint, and remove the workspace memory column.

ALTER TABLE workspaces
    DROP COLUMN IF EXISTS memory;

DROP TABLE IF EXISTS agent_daily_memories;
DROP TABLE IF EXISTS user_memories;

ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS uq_agents_workspace_id_id;

CREATE TABLE agent_user_memories (
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    content      text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, user_id)
);

CREATE INDEX idx_agent_user_memories_workspace_id ON agent_user_memories(workspace_id);
