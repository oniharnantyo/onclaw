-- Agent memory (design.md D1/D5): three persistent memory scopes replacing the
-- dormant agent_user_memories stub (no runtime or API writer ever existed, so
-- dropping it loses nothing). USER memory is one document per (workspace,
-- user); WORKSPACE memory is a column on the existing workspace row. Daily
-- memories are one row per (workspace, agent, date) with the composite PK as
-- the upsert conflict target. The composite FK to agents(workspace_id, id),
-- backed by the new UNIQUE constraint below, makes cross-workspace daily rows
-- unwritable at the data layer, not merely unqueried.

ALTER TABLE agents
    ADD CONSTRAINT uq_agents_workspace_id_id UNIQUE (workspace_id, id);

CREATE TABLE user_memories (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content      text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE TABLE agent_daily_memories (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL,
    memory_date  date NOT NULL,
    content      text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, agent_id, memory_date),
    CONSTRAINT fk_agent_daily_memories_agents FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);

ALTER TABLE workspaces
    ADD COLUMN memory text NOT NULL DEFAULT '';

DROP TABLE IF EXISTS agent_user_memories;
