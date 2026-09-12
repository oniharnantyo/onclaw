-- Agent session index (agent-session-index design D1): a durable per-user
-- index of agent chat sessions so sidebar history follows the account, not
-- the browser. Mirror of channel_work_sessions: one row per
-- (workspace, agent, session) with a surrogate uuid id, the birth-derived
-- title, and soft delete (deleted_at) so listings can hide a session while
-- its transcript events and checkpoints stay intact.

CREATE TABLE agent_sessions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id       uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id     text NOT NULL,
    title          text NOT NULL DEFAULT '',
    deleted_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_active_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id, session_id)
);
CREATE INDEX idx_agent_sessions_user_listing
    ON agent_sessions(workspace_id, agent_id, user_id, last_active_at DESC);
