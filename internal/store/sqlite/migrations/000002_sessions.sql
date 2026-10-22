-- Schema v2: session registry backing the chat menu's per-agent session
-- list ("sessions organized per agent", with latest activity). The
-- conversation content stays in session_events; this table is the queryable
-- index. Conventions as in schema v1: store-assigned hex IDs, RFC3339Nano
-- TEXT timestamps stamped by the store layer.

CREATE TABLE sessions (
    id               TEXT PRIMARY KEY,
    agent_id         TEXT NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    last_activity_at TEXT NOT NULL
);

CREATE INDEX idx_sessions_agent_activity
    ON sessions (agent_id, last_activity_at DESC);
