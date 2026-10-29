-- Schema v16: derived agent memory state (add-agent-memory design D1/D6/D8).
-- Four tables, each owned by one agent and removed with it (FK cascade under
-- the connection's foreign_keys(1), mirroring schedules): the recall traces
-- every memory_search call writes (the consolidation signals' only source,
-- design D6), the per-note retention state the sweep maintains (design D9),
-- the promotion staging state for daily-note lines (design D8/D10), and the
-- per-agent sweep heartbeat (design D13). The workspace memory/ files are the
-- source of truth: no column anywhere holds note content — every row is
-- derived, rebuildable state. JSON-in-TEXT columns hold hit lists, signal
-- sets, and sweep counts (schema v1 conventions); timestamps are RFC3339Nano
-- TEXT, nullable where the event has not happened yet. No backfill; rollback
-- is DROP TABLE.
CREATE TABLE memory_traces (
    id         TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL DEFAULT '',
    turn_id    TEXT NOT NULL DEFAULT '',
    origin     TEXT NOT NULL CHECK (origin IN ('user','scheduler','subagent','other')),
    query      TEXT NOT NULL,
    hits       TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_memory_traces_agent_created ON memory_traces(agent_id, created_at);

-- One row per note the sweep has scored; (agent_id, note_name) is the key —
-- the note's text lives in the workspace file, never here.
CREATE TABLE memory_note_state (
    agent_id         TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    note_name        TEXT NOT NULL,
    retention        REAL NOT NULL DEFAULT 0,
    recall_count     INTEGER NOT NULL DEFAULT 0 CHECK (recall_count >= 0),
    distinct_queries INTEGER NOT NULL DEFAULT 0 CHECK (distinct_queries >= 0),
    last_recall      TEXT,
    first_seen       TEXT NOT NULL,
    PRIMARY KEY (agent_id, note_name)
);

-- One row per promotion candidate; (agent_id, source_ref) is the key — a
-- daily-note line's Source reference (design D10) is staged once.
CREATE TABLE memory_staging (
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    source_ref   TEXT NOT NULL,
    signals      TEXT NOT NULL DEFAULT '{}',
    first_staged TEXT NOT NULL,
    promoted     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, source_ref)
);

-- One row per agent: the sweep's ingest watermark and its heartbeat
-- (design D13 — a dead sweep must be visible, not silent).
CREATE TABLE memory_sweeps (
    agent_id    TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    watermark   TEXT,
    last_start  TEXT,
    last_finish TEXT,
    outcome     TEXT NOT NULL DEFAULT '',
    counts      TEXT NOT NULL DEFAULT '{}'
);
