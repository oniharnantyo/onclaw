-- Schema v1: initial single-user layout (de-tenanted from the donor
-- multi-tenant schema). JSON-in-TEXT columns hold lists and maps (SQLite has
-- no array type); timestamps are RFC3339Nano TEXT stamped by the store layer;
-- booleans are 0/1 INTEGER. IDs are store-assigned random hex; skills are
-- keyed by name (the registry indexes on-disk skill directories).

CREATE TABLE agents (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    provider_id   TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    temperature   REAL NOT NULL DEFAULT 1.0,
    max_tokens    INTEGER NOT NULL DEFAULT 0,
    tools         TEXT NOT NULL DEFAULT '[]',
    skills        TEXT NOT NULL DEFAULT '[]',
    mcp_servers   TEXT NOT NULL DEFAULT '[]',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE providers (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL DEFAULT '',
    api_key    TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT uq_providers_name UNIQUE (name)
);

CREATE TABLE mcp_servers (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    transport    TEXT NOT NULL DEFAULT '',
    command      TEXT NOT NULL DEFAULT '',
    args         TEXT NOT NULL DEFAULT '[]',
    env          TEXT NOT NULL DEFAULT '{}',
    url          TEXT NOT NULL DEFAULT '',
    headers      TEXT NOT NULL DEFAULT '{}',
    enabled      INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'unknown',
    status_error TEXT NOT NULL DEFAULT '',
    tool_count   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    CONSTRAINT uq_mcp_servers_name UNIQUE (name)
);

CREATE TABLE hooks (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    event        TEXT NOT NULL DEFAULT '',
    matcher      TEXT NOT NULL DEFAULT '{}',
    handler_type TEXT NOT NULL DEFAULT '',
    config       TEXT NOT NULL DEFAULT '{}',
    timeout_ms   INTEGER NOT NULL DEFAULT 0,
    on_failure   TEXT NOT NULL DEFAULT 'allow',
    enabled      INTEGER NOT NULL DEFAULT 1,
    position     INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'ok',
    status_error TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    CONSTRAINT uq_hooks_name UNIQUE (name)
);

-- Singleton row: id is pinned to 1 by the CHECK constraint.
CREATE TABLE gateway_settings (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    enabled          INTEGER NOT NULL DEFAULT 0,
    default_agent_id TEXT NOT NULL DEFAULT '',
    updated_at       TEXT NOT NULL
);

-- Append-only session event log. seq is allocated by the store at insert
-- time (MAX(seq)+1 per session, inside a single statement under SQLite's
-- single-writer rule), so it is monotonically increasing per session.
-- (session_id, event_id) is unique so retried appends never duplicate.
CREATE TABLE session_events (
    session_id  TEXT NOT NULL,
    event_id    TEXT NOT NULL,
    turn_id     TEXT NOT NULL DEFAULT '',
    seq         INTEGER NOT NULL,
    kind        TEXT NOT NULL,
    payload     BLOB NOT NULL,
    occurred_at TEXT NOT NULL,
    PRIMARY KEY (session_id, seq),
    CONSTRAINT uq_session_events_session_event UNIQUE (session_id, event_id)
);

CREATE TABLE checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    data          BLOB NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- Skill registry indexing the on-disk skill directories; name is the
-- natural key (the directory name under ~/.buddies/skills/).
CREATE TABLE skills (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    version     TEXT NOT NULL DEFAULT '0.1.0',
    source      TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
