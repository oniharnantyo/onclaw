-- Schema v7: the agent runtime port (design D3, D4, D8). Existing agents
-- gain a dedicated subagents toggle (an allowlist cannot express
-- "everything except subagents" under empty-means-all); hooks gain a
-- per-agent assignment layer on top of the unchanged hooks table and an
-- append-only execution audit; tools gain a key–JSON settings table
-- (today the web_search provider chain). Conventions as in schema v1:
-- 0/1 INTEGER booleans, RFC3339Nano TEXT timestamps, JSON in TEXT columns.
-- No data backfill: today's feature rows are inert, and every existing
-- hook (command type) stays valid.

-- Default on (design D3): the ALTER's DEFAULT 1 stamps every pre-existing
-- agent as subagents-enabled; Go-created rows carry the value explicitly.
ALTER TABLE agents ADD COLUMN subagents_enabled INTEGER NOT NULL DEFAULT 1;

-- Ordered per-agent hook assignments (design D4). A pair is unique —
-- re-assigning replaces the position, not the row count. Both FKs cascade:
-- deleting the agent or the hook removes its assignments. An assigned hook
-- leaves the global tier: it applies only on turns of the agents listed
-- here, resolved by position.
CREATE TABLE agent_hooks (
    agent_id TEXT NOT NULL,
    hook_id  TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, hook_id),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE,
    FOREIGN KEY (hook_id) REFERENCES hooks (id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_hooks_agent
    ON agent_hooks (agent_id, position);

-- Append-only audit of hook evaluations (design D4). No FK on hook_id: the
-- audit trail outlives the hook definition, so deleting a hook keeps its
-- history. outcome is one of allowed | blocked | error; error carries the
-- reason when the handler failed or timed out.
CREATE TABLE hook_executions (
    id          TEXT PRIMARY KEY,
    hook_id     TEXT NOT NULL,
    agent_id    TEXT NOT NULL DEFAULT '',
    session_id  TEXT NOT NULL DEFAULT '',
    turn_id     TEXT NOT NULL DEFAULT '',
    event       TEXT NOT NULL DEFAULT '',
    outcome     TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    occurred_at TEXT NOT NULL
);

CREATE INDEX idx_hook_executions_hook
    ON hook_executions (hook_id, occurred_at DESC);

-- Per-tool configuration as key–JSON rows (design D8): the value is an
-- opaque JSON document owned by the feature reading it, today the
-- web_search provider chain (provider order and credentials).
CREATE TABLE tool_settings (
    tool       TEXT PRIMARY KEY,
    settings   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
