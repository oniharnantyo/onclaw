-- Schema v22: notification settings (add-desktop-notifications design D4).
-- One singleton row (id = 1): the master toggle plus one toggle per trigger
-- category (turns, attention, schedules, groups). Absence of a row is a
-- normal state — readers apply the all-on defaults — so the migration only
-- creates the table, no backfill row. Booleans are INTEGER 0/1 and
-- timestamps are TEXT (RFC3339Nano) like every other table. Rollback is
-- DROP TABLE.

CREATE TABLE notification_settings (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    enabled    INTEGER NOT NULL DEFAULT 1,
    turns      INTEGER NOT NULL DEFAULT 1,
    attention  INTEGER NOT NULL DEFAULT 1,
    schedules  INTEGER NOT NULL DEFAULT 1,
    groups     INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
