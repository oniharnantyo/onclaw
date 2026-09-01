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
