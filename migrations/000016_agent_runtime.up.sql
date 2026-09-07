-- Agents: context_window and denylist capability arrays replace former allowlists
ALTER TABLE agents
    ADD COLUMN context_window bigint CHECK (context_window > 0),
    ADD COLUMN disabled_tools text[] NOT NULL DEFAULT '{}',
    ADD COLUMN disabled_skills text[] NOT NULL DEFAULT '{}',
    ADD COLUMN disabled_mcps text[] NOT NULL DEFAULT '{}';

ALTER TABLE agents
    DROP COLUMN tools,
    DROP COLUMN skills,
    DROP COLUMN mcp;

-- Workspaces: description column
ALTER TABLE workspaces
    ADD COLUMN description text;

-- Workspace skills table dropped
DROP TABLE IF EXISTS workspace_skills;

-- Session event log (append-only, workspace-scoped)
CREATE TABLE session_events (
    session_id   text NOT NULL,
    event_id     text NOT NULL,
    turn_id      text NOT NULL DEFAULT '',
    seq          bigint NOT NULL,
    kind         text NOT NULL,
    payload      bytea NOT NULL,
    occurred_at  timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    PRIMARY KEY (session_id, event_id)
);

CREATE INDEX idx_session_events_workspace_id ON session_events(workspace_id);
CREATE INDEX idx_session_events_session_id_seq ON session_events(session_id, seq);
CREATE INDEX idx_session_events_session_id_kind ON session_events(session_id, kind);

-- Session checkpoints (interrupt checkpoints)
CREATE TABLE session_checkpoints (
    checkpoint_id text PRIMARY KEY,
    data          bytea NOT NULL
);
