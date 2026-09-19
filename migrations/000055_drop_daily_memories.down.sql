-- Reverse 000055 (integrate-agent-zero-memory D14): recreate
-- agent_daily_memories EMPTY with its original 000025 DDL. Dropped rows are
-- intentionally not restored (D14) — the down path exists so the 000025-era
-- schema stays reproducible, not to recover content. The composite FK needs
-- the agents(workspace_id, id) UNIQUE constraint, which 000025 added and
-- only 000025's own down removes, so it is present at this point in the
-- migration history.

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
