-- Schema v13: schedules (add-scheduler design D3/D9). One row per recurring
-- agent run: a five-field cron expression evaluated in the host's local time
-- plus the persisted next_fire_at slot the scheduler's tick advances. The
-- foreign-key cascade removes a schedule with its agent (under the
-- connection's foreign_keys(1), mirroring mcp_oauth_tokens). name carries
-- COLLATE NOCASE so the unique index rejects duplicates case-insensitively.
-- No backfill; rollback is DROP TABLE.
CREATE TABLE schedules (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL COLLATE NOCASE,
    agent_id            TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    prompt              TEXT NOT NULL,
    cron                TEXT NOT NULL,
    enabled             INTEGER NOT NULL DEFAULT 1,
    next_fire_at        TEXT,
    last_run_at         TEXT,
    last_run_status     TEXT NOT NULL DEFAULT '',
    last_run_error      TEXT NOT NULL DEFAULT '',
    last_run_session_id TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_schedules_name ON schedules(name);
CREATE INDEX idx_schedules_due ON schedules(enabled, next_fire_at);
