-- Agent todos (adopt-assistant-ui-elements D5): the durable current-state
-- store behind the todo_write/todo_read tools. The transcript event log keeps
-- the revision history (cards render from the tool-call args); this table is
-- the queryable present — one row per (session, item), so open items are an
-- indexed read, never a payload scan.
--
-- session_id is a logical reference to agent_sessions.session_id (house
-- convention — session_events precedent): no hard FK, so a todo row never
-- constrains the session index and the (session_id, item_key) uniqueness is
-- global across the table. Workspace and agent are hard FKs instead — tenant
-- partition plus cascade deletion with their workspace or agent.
--
-- revision is the session-wide counter stamped on every row of the write that
-- bumped it; a session's current revision reads as MAX(revision) over its
-- rows. A rewrite that clears every item leaves no rows, so the visible
-- counter restarts — the transcript remains the revision history.

CREATE TABLE agent_todos (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_id   text NOT NULL,
    item_key     text NOT NULL,
    item_text    text NOT NULL,
    status       text NOT NULL CHECK (status IN ('pending', 'active', 'done', 'failed')),
    reason       text,
    revision     bigint NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, item_key)
);

-- The open-items query (adopt-assistant-ui-elements spec agent-todos:
-- "a query, not a scan") and the runner's per-turn summary read through this
-- index; the workspace prefix is the tenant partition.
CREATE INDEX idx_agent_todos_open_items
    ON agent_todos(workspace_id, agent_id, status);
