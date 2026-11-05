-- Schema v20: agent groups (add-agent-groups design D1). Six column-oriented
-- tables — no JSON-blob projections: the group aggregate (id is a slug-like
-- store-assigned hex, lead_agent_id nullable until the first member arrives
-- per design D5), the membership roster (PK(group_id, agent_id): one row per
-- member), the shared transcript (per-group seq allocated by the store,
-- PK(group_id, seq), UNIQUE(group_id, message_id): a stable id per authored
-- message so copy-back upserts on regenerate), the acceptance cursor
-- (PK(group_id, agent_id), one read position + summary version per member),
-- the task system (task_id PK, 7-state CHECK, revision-guarded), and the
-- versioned transcript summary (group_id PK). Message kind/author-kind and
-- task state carry CHECK vocabularies. Child rows cascade on group delete;
-- agent-id columns outside the roster (author, owner, lead) are plain TEXT —
-- agent deletion cleans memberships and pauses tasks through the service
-- (spec: agent workspace lifecycle), so transcripts and tasks must survive.
-- No backfill; rollback is DROP TABLE in reverse creation order.

CREATE TABLE agent_groups (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    instructions  TEXT NOT NULL DEFAULT '',
    lead_agent_id TEXT REFERENCES agents(id),
    archived      INTEGER NOT NULL DEFAULT 0,
    revision      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE agent_group_members (
    group_id TEXT NOT NULL REFERENCES agent_groups(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    added_at TEXT NOT NULL,
    PRIMARY KEY (group_id, agent_id)
);

CREATE TABLE agent_group_messages (
    group_id       TEXT NOT NULL REFERENCES agent_groups(id) ON DELETE CASCADE,
    message_id     TEXT NOT NULL,
    seq            INTEGER NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('message', 'assigned', 'result', 'error', 'change')),
    author_kind    TEXT NOT NULL CHECK (author_kind IN ('user', 'agent', 'coordinator')),
    author_agent_id TEXT,
    task_id        TEXT,
    payload        BLOB,
    occurred_at    TEXT NOT NULL,
    PRIMARY KEY (group_id, seq),
    UNIQUE (group_id, message_id)
);

CREATE TABLE agent_group_contexts (
    group_id        TEXT NOT NULL REFERENCES agent_groups(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL,
    session_id      TEXT NOT NULL DEFAULT '',
    through_seq     INTEGER NOT NULL DEFAULT 0,
    summary_version INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, agent_id)
);

CREATE TABLE agent_group_tasks (
    task_id            TEXT PRIMARY KEY,
    group_id           TEXT NOT NULL REFERENCES agent_groups(id) ON DELETE CASCADE,
    parent_task_id     TEXT,
    root_task_id       TEXT NOT NULL DEFAULT '',
    owner_agent_id     TEXT,
    request_message_id TEXT NOT NULL DEFAULT '',
    instruction        TEXT NOT NULL DEFAULT '',
    expected_result    TEXT NOT NULL DEFAULT '',
    state              TEXT NOT NULL CHECK (state IN ('queued', 'running', 'waiting', 'paused', 'completed', 'failed', 'cancelled')),
    stop_reason        TEXT,
    revision           INTEGER NOT NULL DEFAULT 0,
    assignment_count   INTEGER NOT NULL DEFAULT 0,
    error              TEXT
);

CREATE TABLE agent_group_summaries (
    group_id    TEXT PRIMARY KEY REFERENCES agent_groups(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL DEFAULT 0,
    through_seq INTEGER NOT NULL DEFAULT 0,
    text        TEXT NOT NULL DEFAULT ''
);
